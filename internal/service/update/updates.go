package update

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

const (
	switchedOff     = "update checks are switched off in runner.yaml"
	notFromARelease = "this is a development build, so there is no release to compare it against"
)

type updatesService struct {
	releases repository.Release
	build    entity.Build
	cfg      config.Update
	now      func() time.Time

	mu     sync.Mutex
	update entity.Update
}

func New(releases repository.Release, build entity.Build, cfg config.Update) service.Updates {
	return &updatesService{
		releases: releases,
		build:    build,
		cfg:      cfg,
		now:      func() time.Time { return time.Now().UTC() },
		update:   initial(build, cfg),
	}
}

func initial(build entity.Build, cfg config.Update) entity.Update {
	switch {
	case !cfg.Check:
		return entity.Update{State: entity.UpdateOff, Detail: switchedOff}
	case !build.Released():
		return entity.Update{State: entity.UpdateOff, Detail: notFromARelease}
	default:
		return entity.Update{State: entity.UpdateUnchecked}
	}
}

func (s *updatesService) Run(ctx context.Context) {
	if s.Report().State == entity.UpdateOff {
		<-ctx.Done()

		return
	}

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		s.Check(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *updatesService) Report() entity.Update {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.update
}

func (s *updatesService) Check(ctx context.Context) entity.Update {
	if current := s.Report(); current.State == entity.UpdateOff {
		return current
	}

	latest, err := s.releases.Latest(ctx)
	if err != nil {
		return s.unresolved(ctx, err)
	}

	update := entity.Update{State: entity.UpdateCurrent, CheckedAt: s.now()}

	if latest.NewerThan(s.build) {
		update.State = entity.UpdateAvailable
		update.Latest = latest.Version
		update.URL = latest.URL
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.update = update

	return update
}

func (s *updatesService) unresolved(ctx context.Context, err error) entity.Update {
	logging.From(ctx).DebugContext(
		ctx, "the runner could not look up the latest release", slog.String("error", err.Error()),
	)

	s.mu.Lock()
	defer s.mu.Unlock()

	answered := !s.update.CheckedAt.IsZero()

	if answered && errors.Is(err, entity.ErrReleaseUnavailable) {
		s.update.Detail = err.Error()

		return s.update
	}

	s.update = entity.Update{State: entity.UpdateUnknown, Detail: err.Error()}

	return s.update
}
