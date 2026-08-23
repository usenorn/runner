package worktree_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func onABranch(t *testing.T) (string, string, string, string) {
	t.Helper()

	source, bare := origin(t)
	worktrees := maker(t)
	ctx := context.Background()

	base := git(t, source, "rev-parse", "HEAD")
	dest := filepath.Join(t.TempDir(), "work")

	if err := worktrees.Add(ctx, source, dest, base); err != nil {
		t.Fatalf("add a worktree: %v", err)
	}

	if err := worktrees.Branch(ctx, dest, "norn/NORN-54/runner"); err != nil {
		t.Fatalf("branch the worktree: %v", err)
	}

	return source, bare, dest, base
}

func TestABranchPushedFromAWorktreeReachesTheRemoteEverybodyElsePullsFrom(t *testing.T) {
	source, bare, dest, _ := onABranch(t)
	worktrees := maker(t)
	ctx := context.Background()

	commit(t, dest, "b.txt", "two\n")

	url, err := worktrees.Remote(ctx, source)
	if err != nil {
		t.Fatalf("read where the repository pushes to: %v", err)
	}

	if err := worktrees.Push(ctx, dest, url, "norn/NORN-54/runner"); err != nil {
		t.Fatalf("push the branch: %v", err)
	}

	if got := git(t, bare, "rev-parse", "norn/NORN-54/runner"); got == "" {
		t.Fatal(
			"the branch never reached the remote, so nobody but this machine can see the work",
		)
	}

	if git(t, bare, "rev-parse", "norn/NORN-54/runner") != git(t, dest, "rev-parse", "HEAD") {
		t.Fatal("the remote branch is not the commit the run finished on")
	}
}

func TestOnlyWhatHappenedOnTheBranchIsCountedAsWhatTheRunChanged(t *testing.T) {
	_, _, dest, base := onABranch(t)
	worktrees := maker(t)
	ctx := context.Background()

	commit(t, dest, "b.txt", "two\nthree\n")
	commit(t, dest, "c.txt", "four\n")

	commits, err := worktrees.Commits(ctx, dest, base)
	if err != nil {
		t.Fatalf("count the commits: %v", err)
	}

	if commits != 2 {
		t.Fatalf(
			"the run reads as %d commits and it made 2; the count is what a person sees before "+
				"they open the diff",
			commits,
		)
	}

	stat, err := worktrees.Diffstat(ctx, dest, base)
	if err != nil {
		t.Fatalf("read the diffstat: %v", err)
	}

	if stat.Files != 2 || stat.Additions != 3 || stat.Deletions != 0 {
		t.Fatalf(
			"the diffstat reads %+v and the run added three lines across two files; a review "+
				"screen sorts and totals these, so they have to be the real numbers",
			stat,
		)
	}
}

func TestNothingCommittedBeforeTheRunCountsAsSomethingTheRunDid(t *testing.T) {
	source, _, _, _ := onABranch(t)
	worktrees := maker(t)
	ctx := context.Background()

	commit(t, source, "earlier.txt", "not this run\n")

	head := git(t, source, "rev-parse", "HEAD")
	dest := filepath.Join(t.TempDir(), "later")

	if err := worktrees.Add(ctx, source, dest, head); err != nil {
		t.Fatalf("add a worktree at the later commit: %v", err)
	}

	commits, err := worktrees.Commits(ctx, dest, head)
	if err != nil {
		t.Fatalf("count the commits: %v", err)
	}

	if commits != 0 {
		t.Fatalf(
			"a run that committed nothing reads as %d commits, so it would be reported as work "+
				"nobody did",
			commits,
		)
	}
}

func TestTheAddressToPushToComesFromTheRepositoryAndNotTheCopyOfIt(t *testing.T) {
	source, bare, _, base := onABranch(t)
	worktrees := maker(t)
	ctx := context.Background()

	copied := filepath.Join(t.TempDir(), "clone")

	if err := worktrees.Clone(ctx, source, copied, base); err != nil {
		t.Fatalf("clone the repository: %v", err)
	}

	held, err := worktrees.Remote(ctx, source)
	if err != nil {
		t.Fatalf("read where the repository pushes to: %v", err)
	}

	if held != bare {
		t.Fatalf("the repository pushes to %q and its remote is %q", held, bare)
	}

	copiedFrom, err := worktrees.Remote(ctx, copied)
	if err != nil {
		t.Fatalf("read where the copy pushes to: %v", err)
	}

	if copiedFrom != source {
		t.Fatalf(
			"a cloned workspace points at %q; this is the whole reason the address is read off "+
				"the original, because pushing to the copy's own origin writes into the "+
				"developer's checkout instead of the forge",
			copiedFrom,
		)
	}
}

func TestARepositoryWithNoRemoteSaysSoRatherThanPushingNowhere(t *testing.T) {
	worktrees := maker(t)
	ctx := context.Background()

	alone := filepath.Join(t.TempDir(), "alone")

	write(t, filepath.Join(alone, "a.txt"), "one\n")
	git(t, alone, "init", "-q", "-b", "main")
	git(t, alone, "add", "a.txt")
	git(t, alone, "commit", "-q", "-m", "one")

	if _, err := worktrees.Remote(ctx, alone); err == nil {
		t.Fatal(
			"a repository with no remote answered an address anyway, so the run would report a " +
				"branch it never pushed",
		)
	}
}

func TestAPushIsRefusedRatherThanOverwritingWhatSomebodyElsePushed(t *testing.T) {
	source, bare, dest, base := onABranch(t)
	worktrees := maker(t)
	ctx := context.Background()

	commit(t, dest, "b.txt", "ours\n")

	theirs := filepath.Join(t.TempDir(), "theirs")

	if err := worktrees.Add(ctx, source, theirs, base); err != nil {
		t.Fatalf("add a second worktree: %v", err)
	}

	git(t, theirs, "switch", "-q", "-c", "somebody-else")
	commit(t, theirs, "b.txt", "theirs\n")
	git(t, theirs, "push", "-q", bare, "HEAD:refs/heads/norn/NORN-54/runner")

	err := worktrees.Push(ctx, dest, bare, "norn/NORN-54/runner")
	if err == nil {
		t.Fatal(
			"pushing over a branch that moved on succeeded, so work somebody else pushed would " +
				"be gone with no trace of it",
		)
	}

	if !strings.Contains(git(t, bare, "log", "-1", "--format=%s", "norn/NORN-54/runner"), "b.txt") {
		t.Fatal("the remote branch is no longer what the other worktree pushed")
	}
}

func TestTheDiffOfABranchIsWhatTheRunActuallyWrote(t *testing.T) {
	_, _, dest, base := onABranch(t)
	worktrees := maker(t)
	ctx := context.Background()

	commit(t, dest, "b.txt", "two\n")

	patch, err := worktrees.Patch(ctx, dest, base)
	if err != nil {
		t.Fatalf("read the diff: %v", err)
	}

	if !strings.Contains(string(patch), "b.txt") || !strings.Contains(string(patch), "+two") {
		t.Fatalf(
			"the diff a reviewer opens does not carry what the run wrote:\n%s", string(patch),
		)
	}
}

func TestAWorktreeWithNothingLeftBehindReadsAsClean(t *testing.T) {
	_, _, dest, _ := onABranch(t)
	worktrees := maker(t)
	ctx := context.Background()

	commit(t, dest, "b.txt", "two\n")

	changed, err := worktrees.Changed(ctx, dest)
	if err != nil {
		t.Fatalf("read what changed: %v", err)
	}

	untracked, err := worktrees.Untracked(ctx, dest)
	if err != nil {
		t.Fatalf("read what is untracked: %v", err)
	}

	if len(changed)+len(untracked) != 0 {
		t.Fatalf(
			"a worktree whose work is committed reads as dirty (%v, %v), so every finished run "+
				"would be sent back to commit nothing",
			changed, untracked,
		)
	}

	write(t, filepath.Join(dest, "forgotten.txt"), "not committed\n")

	untracked, err = worktrees.Untracked(ctx, dest)
	if err != nil {
		t.Fatalf("read what is untracked: %v", err)
	}

	if len(untracked) != 1 {
		t.Fatalf(
			"a file the agent never committed does not show up as left behind: %v; it would be "+
				"thrown away with the workspace and nobody would know",
			untracked,
		)
	}
}
