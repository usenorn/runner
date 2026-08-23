package execution

import (
	"context"
	"log/slog"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
)

func (s *executionsService) collect(ctx context.Context) {
	if s.runner.Retention.SweepInterval <= 0 {
		<-ctx.Done()

		return
	}

	ticker := time.NewTicker(s.runner.Retention.SweepInterval)
	defer ticker.Stop()

	for {
		s.sweep(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *executionsService) sweep(ctx context.Context) {
	usage, err := s.runs.Usage(ctx)
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not read what its runs are taking up",
			slog.String("error", err.Error()),
		)

		return
	}

	usage = s.marked(usage)
	retention := s.runner.Retention
	now := s.now()

	retired := entity.Retirable(usage, now, retention.WorkspaceAfterDone)
	reaped := entity.Reapable(usage, now, retention.RunsMaxAge, retention.RunsMaxDisk)

	for _, name := range retired {
		s.retire(ctx, name)
	}

	for _, name := range reaped {
		s.reap(ctx, name)
	}

	if len(retired) > 0 {
		usage = s.remeasured(ctx, usage)
	}

	kept := entity.Without(usage, reaped)
	left := entity.Occupied(kept)

	if left > retention.RunsMaxDisk {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine keeps less room for its runs than they are taking, and everything over "+
				"it is still working",
			slog.Int64("bytes", left),
			slog.Int64("budget", retention.RunsMaxDisk),
		)
	}

	s.swept(entity.RunsReport{
		Runs:    len(kept),
		Bytes:   left,
		SweptAt: now,
	})
}

func (s *executionsService) retire(ctx context.Context, executionID string) {
	if err := s.teardown(ctx, executionID); err != nil {
		s.complain(ctx, executionID, err)

		return
	}

	if err := s.runs.Retire(ctx, executionID); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not clear away what a run left behind",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)

		return
	}

	s.record(ctx, executionID, entity.TimelineEntry{
		Kind:     channelv1.EventPhase,
		Reason:   entity.Retired(s.runner.Retention.WorkspaceAfterDone),
		Occurred: s.now(),
	})
}

func (s *executionsService) reap(ctx context.Context, executionID string) {
	if err := s.teardown(ctx, executionID); err != nil {
		s.complain(ctx, executionID, err)

		return
	}

	if err := s.runs.Remove(ctx, executionID); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not take an old run off the disk",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)

		return
	}

	logging.From(ctx).InfoContext(
		ctx,
		"an old run was taken off the disk; what it did is on its branches and with norn",
		slog.String("execution_id", executionID),
	)
}

func (s *executionsService) remeasured(
	ctx context.Context,
	measured []entity.RunUsage,
) []entity.RunUsage {
	usage, err := s.runs.Usage(ctx)
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not read back what its runs take up now it has cleared some away",
			slog.String("error", err.Error()),
		)

		return measured
	}

	return usage
}

func (s *executionsService) marked(usage []entity.RunUsage) []entity.RunUsage {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, held := range usage {
		if _, holding := s.held[held.Name]; holding {
			usage[index].Held = true
		}
	}

	return usage
}

func (s *executionsService) swept(report entity.RunsReport) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.usage = report
}
