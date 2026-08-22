package run_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	runrepo "github.com/usenorn/runner/internal/repository/run"
)

func store(t *testing.T) (*statedir.Dir, context.Context) {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("create the state directory: %v", err)
	}

	return dir, context.Background()
}

func snapshot(name, run string) entity.Snapshot {
	return entity.Snapshot{
		Name:         name,
		Run:          run,
		Workspace:    filepath.Join(run, entity.SnapshotWorkspaceDir),
		IssueKey:     "NORN-46",
		Attempt:      1,
		CodebaseID:   uuid.MustParse("2ac0ee31-8a71-4b57-9c22-2f4c8f0f8ad1"),
		CodebaseRoot: "/w",
		Repositories: []entity.SnapshotRepository{{
			Name:    "runner",
			RelPath: "runner",
			Kind:    entity.RepositoryNormal,
			Source:  "/w/runner",
			Path:    filepath.Join(run, entity.SnapshotWorkspaceDir, "runner"),
			Mode:    entity.GitModeWorktree,
			Base:    entity.BaseOriginDefault,
			BaseSHA: "3a91c2f8b7d64e05aa1188b0c4e2f9d1a7c65e30",
			Branch:  "norn/NORN-46/runner",
			Local: &entity.LocalPatch{
				BaseSHA:   "3a91c2f8b7d64e05aa1188b0c4e2f9d1a7c65e30",
				Commit:    "88de104f0b2c4a7e91d3",
				PatchFile: "local-changes/runner.patch",
				Files:     3,
			},
		}},
		Shared:   []entity.SharedFile{{RelPath: "AGENTS.md", Method: entity.MaterialiseReflink, Size: 42}},
		Bytes:    42,
		Warnings: []string{"one thing worth saying"},
		TakenAt:  time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC),
		Took:     1800 * time.Millisecond,
	}
}

func TestASnapshotComesBackExactlyAsItWasWrittenDown(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	run, err := runs.Prepare(ctx, "snap-NORN-46-1")
	if err != nil {
		t.Fatalf("prepare a run: %v", err)
	}

	want := snapshot("snap-NORN-46-1", run)

	if err := runs.Save(ctx, want); err != nil {
		t.Fatalf("save the snapshot: %v", err)
	}

	got, err := runs.Load(ctx, "snap-NORN-46-1")
	if err != nil {
		t.Fatalf("load the snapshot: %v", err)
	}

	held, wanted := got.Repositories[0], want.Repositories[0]

	if *held.Local != *wanted.Local {
		t.Fatalf("the local changes came back as %+v", *held.Local)
	}

	held.Local, wanted.Local = nil, nil

	if held != wanted {
		t.Fatalf("the repository came back as %+v", held)
	}

	if got.IssueKey != want.IssueKey || got.CodebaseID != want.CodebaseID || got.Took != want.Took {
		t.Fatalf("the snapshot came back as %+v", got)
	}

	if len(got.Shared) != 1 || got.Shared[0] != want.Shared[0] || got.Bytes != want.Bytes {
		t.Fatalf("the shared files came back as %+v", got.Shared)
	}

	if held.Source != "/w/runner" || held.Branch != "norn/NORN-46/runner" {
		t.Fatalf(
			"the record does not name where the repository came from or what branch it is on; "+
				"teardown reads this file to give the original its worktree back: %+v",
			held,
		)
	}
}

func TestPreparingARunTwiceIsRefusedRatherThanOverwritingWhatIsThere(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Prepare(ctx, "snap-NORN-46-1"); err != nil {
		t.Fatalf("prepare a run: %v", err)
	}

	if _, err := runs.Prepare(ctx, "snap-NORN-46-1"); !errors.Is(err, entity.ErrSnapshotExists) {
		t.Fatalf(
			"preparing the same run twice returned %v; overwriting one would strand the worktrees "+
				"the first one is still holding",
			err,
		)
	}
}

func TestARunDirectoryHoldsAWorkspaceAndItsMetadataFromTheStart(t *testing.T) {
	dir, ctx := store(t)

	run, err := runrepo.New(dir).Prepare(ctx, "snap-NORN-46-1")
	if err != nil {
		t.Fatalf("prepare a run: %v", err)
	}

	for _, child := range []string{entity.SnapshotWorkspaceDir, entity.SnapshotMetadataDir} {
		if _, err := os.Stat(filepath.Join(run, child)); err != nil {
			t.Fatalf("%s is missing from a prepared run: %v", child, err)
		}
	}
}

func TestListingSkipsARunThatWasNeverFinishedAndKeepsTheRest(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	for _, name := range []string{"snap-NORN-46-1", "snap-NORN-47-1"} {
		run, err := runs.Prepare(ctx, name)
		if err != nil {
			t.Fatalf("prepare %s: %v", name, err)
		}

		if name == "snap-NORN-46-1" {
			continue
		}

		if err := runs.Save(ctx, snapshot(name, run)); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}

	held, err := runs.List(ctx)
	if err != nil {
		t.Fatalf("list the runs: %v", err)
	}

	if len(held) != 1 || held[0].Name != "snap-NORN-47-1" {
		t.Fatalf("the list came back as %+v", held)
	}
}

func TestRemovingARunThatIsNotThereSaysSoRatherThanPretending(t *testing.T) {
	dir, ctx := store(t)

	if err := runrepo.New(dir).Remove(ctx, "snap-NORN-99-1"); !errors.Is(err, entity.ErrSnapshotMissing) {
		t.Fatalf("removing a run that does not exist returned %v", err)
	}
}
