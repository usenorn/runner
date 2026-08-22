package codebase

import (
	"context"
	"log/slog"
	"time"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
)

func (s *codebasesService) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.RescanInterval)
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

func (s *codebasesService) sweep(ctx context.Context) {
	held, err := s.inventories.List(ctx)
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx, "the runner could not read what folders it holds", slog.String("error", err.Error()),
		)

		return
	}

	for _, codebase := range held {
		if err := s.revisit(ctx, codebase); err != nil {
			logging.From(ctx).WarnContext(
				ctx,
				"the runner could not re-read a connected folder",
				slog.String("codebase", codebase.Name),
				slog.String("root", codebase.RootPath),
				slog.String("error", err.Error()),
			)
		}
	}
}

func (s *codebasesService) revisit(ctx context.Context, codebase entity.Codebase) error {
	scan, err := s.Scan(ctx, codebase.RootPath)
	if err != nil {
		return err
	}

	if !entity.DriftBetween(codebase.Reported, scan.Inventory).Any() {
		return nil
	}

	token, err := s.sessions.Access(ctx)
	if err != nil {
		return err
	}

	settled := !entity.DriftBetween(codebase.Confirmed, scan.Inventory).Any()

	reported, err := s.report(ctx, token, codebase, scan.Inventory, settled)
	if err != nil {
		return err
	}

	if reported.Drifted() {
		logging.From(ctx).InfoContext(
			ctx,
			"a connected folder no longer holds what was confirmed",
			slog.String("codebase", reported.Name),
			slog.String("root", reported.RootPath),
			slog.Int("changes", entity.DriftBetween(reported.Confirmed, reported.Reported).Count()),
		)
	}

	return nil
}
