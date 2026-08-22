package snapshot_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/service"
)

func TestAFolderNornWasNeverToldAboutIsRefused(t *testing.T) {
	h := newHarness(t, defaults())
	h.held = nil

	if _, err := h.take(); !errors.Is(err, entity.ErrCodebaseNotConnected) {
		t.Fatalf(
			"snapshotting an unconnected folder returned %v; the confirmed inventory is what says "+
				"which repositories a person agreed to hand to an execution",
			err,
		)
	}
}

func TestASnapshotCanBeTakenFromAnywhereInsideTheConnectedFolder(t *testing.T) {
	h := newHarness(t, defaults())

	taken, err := h.service.Take(context.Background(), service.TakeRequest{
		Path:     filepath.Join(h.root, "runner"),
		IssueKey: "NORN-46",
	})
	if err != nil {
		t.Fatalf("snapshot from inside the folder: %v", err)
	}

	if taken.CodebaseRoot != h.root {
		t.Fatalf("the snapshot names %s as its folder", taken.CodebaseRoot)
	}
}

func TestAnIssueKeyIsRequiredBecauseTheBranchIsNamedAfterIt(t *testing.T) {
	h := newHarness(t, defaults())

	_, err := h.service.Take(context.Background(), service.TakeRequest{Path: h.root})
	if !errors.Is(err, entity.ErrSnapshotIssueKeyEmpty) {
		t.Fatalf("a snapshot with no issue key returned %v", err)
	}
}

func TestAFetchThatCannotReachTheRemoteIsANoteRatherThanAFailure(t *testing.T) {
	h := newHarness(t, defaults())
	h.fetchFails = errors.New("could not resolve host github.com")

	taken, err := h.take()
	if err != nil {
		t.Fatalf(
			"a snapshot failed because the machine is offline: %v; the repository already holds "+
				"everything the execution needs to start",
			err,
		)
	}

	if len(taken.Warnings) == 0 {
		t.Fatalf("the snapshot started from a stale base without saying so")
	}

	if len(taken.Repositories) != 2 {
		t.Fatalf("%d repositories were snapshotted and the folder holds two", len(taken.Repositories))
	}
}

func TestTheFolderOverridesTheRunnerAndTheRunOverridesTheFolder(t *testing.T) {
	h := newHarness(t, defaults())

	if _, err := h.take(); err != nil {
		t.Fatalf("snapshot with the runner's own settings: %v", err)
	}

	if len(h.added) != 2 || len(h.cloned) != 0 {
		t.Fatalf("the runner is configured for worktrees and %d clones were made", len(h.cloned))
	}

	h = newHarness(t, defaults())
	h.settings.GitMode = "clone"

	if _, err := h.take(); err != nil {
		t.Fatalf("snapshot with the folder's settings: %v", err)
	}

	if len(h.cloned) != 2 || len(h.added) != 0 {
		t.Fatalf(
			"the folder asked for clones and %d worktrees were added instead; a folder that says "+
				"worktrees misbehave on this machine has to be believed",
			len(h.added),
		)
	}
}

func TestARunThatWouldOverwriteAnEarlierOneIsRefused(t *testing.T) {
	h := newHarness(t, defaults())

	if _, err := h.take(); err != nil {
		t.Fatalf("take the first snapshot: %v", err)
	}

	if _, err := h.take(); !errors.Is(err, entity.ErrSnapshotExists) {
		t.Fatalf(
			"taking the same snapshot twice returned %v; the first one is still holding worktrees "+
				"in the person's repositories",
			err,
		)
	}
}

func TestNothingIsLeftBehindWhenARepositoryCannotBeCheckedOut(t *testing.T) {
	h := newHarness(t, defaults())
	h.branchFail = errors.New("fatal: cannot lock ref")

	if _, err := h.take(); err == nil {
		t.Fatalf("a snapshot that could not branch was reported as a success")
	}

	run := filepath.Join(h.dir.Runs(), entity.RunNameFor("NORN-46", 1))

	if _, err := os.Stat(run); err == nil {
		t.Fatalf(
			"%s is still on disk after the snapshot failed; a half-built workspace is something a "+
				"person then has to clean up by hand",
			run,
		)
	}

	if len(h.removed) != len(h.added) {
		t.Fatalf(
			"%d worktrees were added and %d were taken back out; every one that was made before "+
				"the failure has to be given back",
			len(h.added), len(h.removed),
		)
	}
}

func TestTakingASnapshotTwiceForTwoAttemptsGivesEachItsOwnRunAndBranches(t *testing.T) {
	h := newHarness(t, defaults())

	first, err := h.take()
	if err != nil {
		t.Fatalf("take the first attempt: %v", err)
	}

	second, err := h.service.Take(context.Background(), service.TakeRequest{
		Path: h.root, IssueKey: "NORN-46", Attempt: 2,
	})
	if err != nil {
		t.Fatalf("take the second attempt: %v", err)
	}

	if first.Name == second.Name || first.Workspace == second.Workspace {
		t.Fatalf("both attempts landed in %s", first.Name)
	}

	if !slices.Contains(h.branched, "norn/NORN-46/runner-r2") {
		t.Fatalf("the second attempt branched %v and none of them is an r2", h.branched)
	}
}

func TestDiscardingASnapshotGivesEveryWorktreeBackBeforeTheFolderGoes(t *testing.T) {
	h := newHarness(t, defaults())

	taken, err := h.take()
	if err != nil {
		t.Fatalf("take a snapshot: %v", err)
	}

	if err := h.service.Discard(context.Background(), taken.Name); err != nil {
		t.Fatalf("discard %s: %v", taken.Name, err)
	}

	if len(h.removed) != 2 {
		t.Fatalf(
			"%d worktrees were taken back out of two; a registration left behind makes the "+
				"person's own repository complain about a folder that is gone",
			len(h.removed),
		)
	}

	if _, err := os.Stat(taken.Run); err == nil {
		t.Fatalf("%s is still on disk after it was discarded", taken.Run)
	}
}

func TestListingSaysWhatThisMachineIsHolding(t *testing.T) {
	h := newHarness(t, defaults())

	if _, err := h.take(); err != nil {
		t.Fatalf("take a snapshot: %v", err)
	}

	held, err := h.service.List(context.Background())
	if err != nil {
		t.Fatalf("list the snapshots: %v", err)
	}

	if len(held) != 1 || held[0].IssueKey != "NORN-46" {
		t.Fatalf("the list came back as %+v", held)
	}
}

func TestAFolderThatHasDriftedIsSnapshottedAsItWasConfirmedAndSaysSo(t *testing.T) {
	h := newHarness(t, defaults())

	drifted := h.held[0]
	drifted.Reported.Repositories = append(drifted.Reported.Repositories, entity.Repository{
		Name: "extra", RelPath: "extra", Kind: entity.RepositoryNormal, DefaultBranch: "main",
	})
	h.held = []entity.Codebase{drifted}

	taken, err := h.take()
	if err != nil {
		t.Fatalf("snapshot a drifted folder: %v", err)
	}

	if len(taken.Repositories) != 2 {
		t.Fatalf(
			"%d repositories were snapshotted; a repository nobody has confirmed must not be "+
				"handed to a coding agent on the strength of a background scan",
			len(taken.Repositories),
		)
	}

	said := false

	for _, warning := range taken.Warnings {
		said = said || strings.Contains(warning, "drifted")
	}

	if !said {
		t.Fatalf("the snapshot used the confirmed inventory without saying the folder had moved on")
	}
}
