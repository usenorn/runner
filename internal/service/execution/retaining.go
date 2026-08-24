package execution

import (
	"context"
	"log/slog"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
)

func (s *executionsService) Retain(
	ctx context.Context,
	executionID string,
	keepUntil time.Time,
) error {
	execution, err := s.runs.LoadTask(ctx, executionID)
	if err != nil {
		return err
	}

	if !keepUntil.After(s.now()) || !keepUntil.After(execution.KeepUntil) {
		return nil
	}

	execution.KeepUntil = keepUntil

	if err := s.runs.SaveTask(ctx, execution); err != nil {
		return err
	}

	s.mu.Lock()

	if held, holding := s.held[executionID]; holding {
		held.KeepUntil = keepUntil
		s.held[executionID] = held
	}

	s.mu.Unlock()

	s.record(ctx, executionID, entity.TimelineEntry{
		Kind:     channelv1.EventPhase,
		Reason:   entity.Kept(keepUntil),
		Occurred: s.now(),
	})

	s.kept(ctx, execution)

	logging.From(ctx).InfoContext(
		ctx,
		"norn asked this machine to keep a run's workspace and previews for longer",
		slog.String("execution_id", executionID),
		slog.Time("keep_until", keepUntil),
	)

	return nil
}
