package inventory_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
)

func store(t *testing.T) (*statedir.Dir, context.Context) {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("create the state directory: %v", err)
	}

	return dir, context.Background()
}

func codebase() entity.Codebase {
	stamped := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)

	inventory := entity.Inventory{
		Name:     "norn",
		RootPath: "/w",
		Repositories: []entity.Repository{{
			Name:          "norn",
			RelPath:       "norn",
			Kind:          entity.RepositoryNormal,
			DefaultBranch: "main",
			Remote: entity.RemoteFingerprint{
				Hash: "abc", Host: "github.com", PathTail: "usenorn/norn",
			},
		}},
		SharedFiles: []string{"AGENTS.md"},
		Runtimes:    []entity.Runtime{entity.RuntimeProcess},
		Tools:       []entity.Tool{{Name: "claude", Version: "2.1.4"}},
		ScannedAt:   stamped,
	}

	return entity.Codebase{
		ID:          uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		Name:        "norn",
		RootPath:    "/w",
		Confirmed:   inventory,
		Reported:    inventory,
		ConfirmedAt: stamped,
		ReportedAt:  stamped,
	}
}

func TestAConnectedFolderIsReadBackExactlyAsItWasWritten(t *testing.T) {
	dir, ctx := store(t)
	inventories := inventoryrepo.New(dir)

	if err := inventories.Save(ctx, codebase()); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := inventories.Load(ctx, "/w")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	want := codebase()

	if loaded.ID != want.ID || loaded.Name != want.Name || loaded.RootPath != want.RootPath {
		t.Fatalf("the codebase came back as %+v", loaded)
	}

	if entity.DriftBetween(want.Confirmed, loaded.Confirmed).Any() {
		t.Fatalf("the repositories did not survive being written and read")
	}

	if loaded.Confirmed.Tools[0].Version != "2.1.4" {
		t.Fatalf("the tool versions did not survive: %+v", loaded.Confirmed.Tools)
	}
}

func TestAnInventoryIsReadableOnlyByTheAccountThatOwnsIt(t *testing.T) {
	dir, ctx := store(t)

	if err := inventoryrepo.New(dir).Save(ctx, codebase()); err != nil {
		t.Fatalf("save: %v", err)
	}

	path := filepath.Join(dir.Codebase(codebase().ID.String()), "inventory.json")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf(
			"the inventory is %o, want 0600; it names every repository on this machine and where "+
				"each one sits",
			mode,
		)
	}
}

func TestAFolderThatWasNeverConnectedIsNotFound(t *testing.T) {
	dir, ctx := store(t)

	if _, err := inventoryrepo.New(dir).Load(ctx, "/elsewhere"); err == nil {
		t.Fatalf("a folder that was never connected loaded successfully")
	}
}

func TestForgettingACodebaseLeavesNothingBehind(t *testing.T) {
	dir, ctx := store(t)
	inventories := inventoryrepo.New(dir)

	if err := inventories.Save(ctx, codebase()); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := inventories.Remove(ctx, codebase().ID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	held, err := inventories.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(held) != 0 {
		t.Fatalf("%d codebases survived being forgotten", len(held))
	}
}
