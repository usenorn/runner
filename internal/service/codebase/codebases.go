package codebase

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

type codebasesService struct {
	scanner      repository.Scanner
	capabilities repository.Capability
	inventories  repository.Inventory
	dashboard    repository.Dashboard
	sessions     service.Sessions
	cfg          config.Codebase
	now          func() time.Time
}

func New(
	scanner repository.Scanner,
	capabilities repository.Capability,
	inventories repository.Inventory,
	dashboard repository.Dashboard,
	sessions service.Sessions,
	cfg config.Codebase,
) service.Codebases {
	return &codebasesService{
		scanner:      scanner,
		capabilities: capabilities,
		inventories:  inventories,
		dashboard:    dashboard,
		sessions:     sessions,
		cfg:          cfg,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (s *codebasesService) List(ctx context.Context) ([]entity.Codebase, error) {
	return s.inventories.List(ctx)
}

func (s *codebasesService) Scan(ctx context.Context, root string) (service.Scan, error) {
	folder, err := s.scanner.Scan(ctx, root, s.cfg.ScanDepth)
	if err != nil {
		return service.Scan{}, err
	}

	inventory := entity.Inventory{
		Name:         filepath.Base(folder.Root),
		RootPath:     folder.Root,
		Repositories: entity.Classify(folder.Root, folder.Repositories),
		SharedFiles:  folder.SharedFiles,
		ScannedAt:    s.now(),
	}

	if err := usable(inventory); err != nil {
		return service.Scan{}, err
	}

	capabilities := s.capabilities.Detect(ctx, s.sessions.Previews().Gateway)
	inventory.Runtimes = capabilities.Runtimes
	inventory.Tools = capabilities.Tools
	inventory.Gateway = capabilities.Gateway

	scan := service.Scan{Inventory: inventory, Warnings: folder.Warnings}

	held, err := s.inventories.Load(ctx, folder.Root)
	if err != nil {
		if !errors.Is(err, entity.ErrCodebaseNotConnected) {
			return service.Scan{}, err
		}

		if err := s.unclaimed(ctx, folder.Root); err != nil {
			return service.Scan{}, err
		}

		return scan, nil
	}

	scan.Connected = true
	scan.CodebaseID = held.ID
	scan.Drift = entity.DriftBetween(held.Confirmed, inventory)
	scan.Reconcile = entity.DriftBetween(held.Reported, inventory).Any()
	scan.Inventory.Name = held.Name

	return scan, nil
}

func (s *codebasesService) Accept(
	ctx context.Context,
	scan service.Scan,
) (entity.Codebase, error) {
	if err := usable(scan.Inventory); err != nil {
		return entity.Codebase{}, err
	}

	token, err := s.sessions.Access(ctx)
	if err != nil {
		return entity.Codebase{}, err
	}

	return s.report(ctx, token, entity.Codebase{RootPath: scan.Inventory.RootPath}, scan.Inventory, true)
}

func (s *codebasesService) report(
	ctx context.Context,
	token string,
	held entity.Codebase,
	inventory entity.Inventory,
	confirming bool,
) (entity.Codebase, error) {
	connected, err := s.dashboard.ConnectCodebase(ctx, token, inventory)
	if err != nil {
		return entity.Codebase{}, err
	}

	now := s.now()

	held.ID = connected.ID
	held.Name = connected.Name
	held.RootPath = inventory.RootPath
	held.Reported = inventory
	held.ReportedAt = now

	if confirming {
		if err := s.confirm(ctx, token, connected); err != nil {
			return entity.Codebase{}, err
		}

		held.Confirmed = inventory
		held.ConfirmedAt = now
	}

	if err := s.inventories.Save(ctx, held); err != nil {
		return entity.Codebase{}, err
	}

	return held, nil
}

func (s *codebasesService) confirm(
	ctx context.Context,
	token string,
	connected repository.ConnectedCodebase,
) error {
	if !connected.Drifted {
		return nil
	}

	if _, err := s.dashboard.ConfirmCodebase(ctx, token, connected.ID); err != nil {
		if errors.Is(err, entity.ErrCodebaseNotDrifted) {
			return nil
		}

		return err
	}

	return nil
}

func (s *codebasesService) unclaimed(ctx context.Context, root string) error {
	held, err := s.inventories.List(ctx)
	if err != nil {
		return err
	}

	for _, codebase := range held {
		if overlaps(codebase.RootPath, root) {
			return entity.ErrCodebaseOverlaps
		}
	}

	return nil
}

func overlaps(one, other string) bool {
	return one == other || under(one, other) || under(other, one)
}

func under(parent, child string) bool {
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

func usable(inventory entity.Inventory) error {
	listed := inventory.Listed()

	switch {
	case len(listed) == 0:
		return entity.ErrCodebaseEmpty
	case len(listed) > entity.MaxRepositories:
		return entity.ErrCodebaseTooLarge
	case strings.TrimSpace(inventory.RootPath) == "":
		return entity.ErrCodebaseRootMissing
	default:
		return nil
	}
}
