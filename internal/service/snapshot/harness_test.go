package snapshot_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
	materialiserrepo "github.com/usenorn/runner/internal/repository/materialiser"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	settingsrepo "github.com/usenorn/runner/internal/repository/settings"
	worktreerepo "github.com/usenorn/runner/internal/repository/worktree"
	"github.com/usenorn/runner/internal/service"
	snapshotsvc "github.com/usenorn/runner/internal/service/snapshot"
)

const baseSHA = "3a91c2f8b7d64e05aa1188b0c4e2f9d1a7c65e30"

func defaults() config.Snapshot {
	return config.Snapshot{
		GitMode:        "worktree",
		Base:           "origin/default",
		LocalChanges:   "exclude",
		Fetch:          true,
		FetchTimeout:   30 * time.Second,
		GitTimeout:     60 * time.Second,
		MaxSharedBytes: 1 << 30,
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

type harness struct {
	t         *testing.T
	service   service.Snapshots
	root      string
	dir       *statedir.Dir
	worktrees *worktreerepo.MockWorktree
	settings  repository.CodebaseSettings
	held      []entity.Codebase

	added      []string
	cloned     []string
	branched   []string
	removed    []string
	fetchFails error
	addFails   error
	branchFail error
}

func newHarness(t *testing.T, cfg config.Snapshot) *harness {
	t.Helper()

	ctrl := gomock.NewController(t)

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("create the state directory: %v", err)
	}

	h := &harness{t: t, root: folder(t), dir: dir}
	h.held = []entity.Codebase{connected(h.root)}

	h.worktrees = worktreerepo.NewMockWorktree(ctrl)
	h.expect()

	inventories := inventoryrepo.NewMockInventory(ctrl)
	inventories.EXPECT().
		List(gomock.Any()).
		DoAndReturn(func(context.Context) ([]entity.Codebase, error) { return h.held, nil }).
		AnyTimes()

	settings := settingsrepo.NewMockSettings(ctrl)
	settings.EXPECT().
		Load(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (repository.CodebaseSettings, error) {
			return h.settings, nil
		}).
		AnyTimes()
	settings.EXPECT().Ignores(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	h.service = snapshotsvc.New(
		h.worktrees, materialiserrepo.New(), settings, inventories, runrepo.New(dir), cfg,
	)

	return h
}

func (h *harness) expect() {
	h.worktrees.EXPECT().
		Head(gomock.Any(), gomock.Any()).
		Return(baseSHA, nil).
		AnyTimes()
	h.worktrees.EXPECT().
		Resolve(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(baseSHA, nil).
		AnyTimes()
	h.worktrees.EXPECT().
		Fetch(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, string) error { return h.fetchFails }).
		AnyTimes()
	h.worktrees.EXPECT().
		Add(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, dest, _ string) error {
			if h.addFails != nil {
				return h.addFails
			}

			h.added = append(h.added, dest)

			return os.MkdirAll(dest, 0o755)
		}).
		AnyTimes()
	h.worktrees.EXPECT().
		Clone(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, dest, _ string) error {
			h.cloned = append(h.cloned, dest)

			return os.MkdirAll(dest, 0o755)
		}).
		AnyTimes()
	h.worktrees.EXPECT().
		Branch(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, name string) error {
			if h.branchFail != nil {
				return h.branchFail
			}

			h.branched = append(h.branched, name)

			return nil
		}).
		AnyTimes()
	h.worktrees.EXPECT().Submodules(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	h.worktrees.EXPECT().Changed(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	h.worktrees.EXPECT().Untracked(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	h.worktrees.EXPECT().
		Remove(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, dest string) error {
			h.removed = append(h.removed, dest)

			return os.RemoveAll(dest)
		}).
		AnyTimes()
}

func folder(t *testing.T) string {
	t.Helper()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary folder: %v", err)
	}

	root := filepath.Join(base, "work")

	writeFile(t, filepath.Join(root, "AGENTS.md"), "rules\n")
	writeFile(t, filepath.Join(root, "norn", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "runner", "main.go"), "package main\n")

	return root
}

func connected(root string) entity.Codebase {
	inventory := entity.Inventory{
		Name:     "work",
		RootPath: root,
		Repositories: []entity.Repository{
			{
				Name:          "norn",
				RelPath:       "norn",
				Kind:          entity.RepositoryNormal,
				DefaultBranch: "main",
				Remote:        entity.RemoteFingerprint{Hash: "abc", Host: "github.com", PathTail: "usenorn/norn"},
			},
			{
				Name:          "runner",
				RelPath:       "runner",
				Kind:          entity.RepositoryNormal,
				DefaultBranch: "main",
				Remote:        entity.RemoteFingerprint{Hash: "def", Host: "github.com", PathTail: "usenorn/runner"},
			},
		},
		SharedFiles: []string{"AGENTS.md"},
	}

	return entity.Codebase{
		ID:        uuid.MustParse("2ac0ee31-8a71-4b57-9c22-2f4c8f0f8ad1"),
		Name:      "work",
		RootPath:  root,
		Confirmed: inventory,
		Reported:  inventory,
	}
}

func (h *harness) take() (entity.Snapshot, error) {
	h.t.Helper()

	return h.service.Take(context.Background(), service.TakeRequest{
		Path:     h.root,
		IssueKey: "NORN-46",
		Attempt:  1,
	})
}
