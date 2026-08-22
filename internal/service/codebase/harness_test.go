package codebase_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	capabilityrepo "github.com/usenorn/runner/internal/repository/capability"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
	scannerrepo "github.com/usenorn/runner/internal/repository/scanner"
	"github.com/usenorn/runner/internal/service"
	codebasesvc "github.com/usenorn/runner/internal/service/codebase"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
)

const root = "/w"

type norn struct {
	connected bool
	id        uuid.UUID
	stored    []entity.Repository
	drifted   bool
	confirms  int
	connects  int
}

func (n *norn) connect(inventory entity.Inventory) repository.ConnectedCodebase {
	n.connects++

	if !n.connected {
		n.connected = true
		n.id = uuid.New()
		n.stored = inventory.Listed()
		n.drifted = false
	} else {
		n.drifted = entity.DriftBetween(
			entity.Inventory{Repositories: n.stored}, inventory,
		).Any()
		n.stored = inventory.Listed()
	}

	return repository.ConnectedCodebase{
		ID:       n.id,
		Name:     inventory.Name,
		RootPath: inventory.RootPath,
		Drifted:  n.drifted,
	}
}

func (n *norn) confirm() (repository.ConnectedCodebase, error) {
	if !n.drifted {
		return repository.ConnectedCodebase{}, entity.ErrCodebaseNotDrifted
	}

	n.confirms++
	n.drifted = false

	return repository.ConnectedCodebase{ID: n.id, RootPath: root, Drifted: false}, nil
}

type harness struct {
	t       *testing.T
	service service.Codebases
	norn    *norn
	held    []entity.Codebase
	finds   []string
	failing error
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	ctrl := gomock.NewController(t)

	h := &harness{t: t, norn: &norn{}, finds: []string{"norn", "runner"}}

	scanner := scannerrepo.NewMockScanner(ctrl)
	scanner.EXPECT().
		Scan(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, at string, _ int) (repository.ScannedFolder, error) {
			if h.failing != nil {
				return repository.ScannedFolder{}, h.failing
			}

			return repository.ScannedFolder{
				Root:         at,
				Repositories: factsFor(at, h.finds),
				SharedFiles:  []string{"AGENTS.md"},
			}, nil
		}).
		AnyTimes()

	capabilities := capabilityrepo.NewMockCapability(ctrl)
	capabilities.EXPECT().
		Detect(gomock.Any()).
		Return(repository.Capabilities{Runtimes: []entity.Runtime{entity.RuntimeProcess}}).
		AnyTimes()

	inventories := inventoryrepo.NewMockInventory(ctrl)
	inventories.EXPECT().
		List(gomock.Any()).
		DoAndReturn(func(context.Context) ([]entity.Codebase, error) { return h.held, nil }).
		AnyTimes()
	inventories.EXPECT().
		Load(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, at string) (entity.Codebase, error) {
			for _, codebase := range h.held {
				if codebase.RootPath == at {
					return codebase, nil
				}
			}

			return entity.Codebase{}, entity.ErrCodebaseNotConnected
		}).
		AnyTimes()
	inventories.EXPECT().
		Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, codebase entity.Codebase) error {
			for index, existing := range h.held {
				if existing.RootPath == codebase.RootPath {
					h.held[index] = codebase

					return nil
				}
			}

			h.held = append(h.held, codebase)

			return nil
		}).
		AnyTimes()

	dashboard := dashboardrepo.NewMockDashboard(ctrl)
	dashboard.EXPECT().
		ConnectCodebase(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, inventory entity.Inventory) (repository.ConnectedCodebase, error) {
			return h.norn.connect(inventory), nil
		}).
		AnyTimes()
	dashboard.EXPECT().
		ConfirmCodebase(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, uuid.UUID) (repository.ConnectedCodebase, error) {
			return h.norn.confirm()
		}).
		AnyTimes()

	sessions := sessionsvc.NewMockSessions(ctrl)
	sessions.EXPECT().Access(gomock.Any()).Return("nrs_token", nil).AnyTimes()

	h.service = codebasesvc.New(scanner, capabilities, inventories, dashboard, sessions, settings())

	return h
}

func settings() config.Codebase {
	return config.Codebase{
		ScanDepth:      entity.ScanDepthDefault,
		RescanInterval: 6 * time.Hour,
		ProbeTimeout:   5 * time.Second,
	}
}

func factsFor(at string, names []string) []entity.GitFacts {
	facts := make([]entity.GitFacts, 0, len(names))

	for _, name := range names {
		dir := filepath.Join(at, name)
		facts = append(facts, entity.GitFacts{
			Dir:            dir,
			GitDir:         filepath.Join(dir, ".git"),
			CommonDir:      filepath.Join(dir, ".git"),
			TopLevel:       dir,
			RemoteURL:      "git@github.com:usenorn/" + name + ".git",
			DefaultBranch:  "main",
			InsideWorkTree: true,
		})
	}

	return facts
}

func (h *harness) scan() service.Scan {
	h.t.Helper()

	scan, err := h.service.Scan(context.Background(), root)
	if err != nil {
		h.t.Fatalf("scan %s: %v", root, err)
	}

	return scan
}

func (h *harness) connect() {
	h.t.Helper()

	if _, err := h.service.Accept(context.Background(), h.scan()); err != nil {
		h.t.Fatalf("connect %s: %v", root, err)
	}
}

func (h *harness) rescan() {
	h.t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	h.service.Run(ctx)
}

func (h *harness) recorded() entity.Codebase {
	h.t.Helper()

	for _, codebase := range h.held {
		if codebase.RootPath == root {
			return codebase
		}
	}

	h.t.Fatalf("nothing was recorded for %s", root)

	return entity.Codebase{}
}
