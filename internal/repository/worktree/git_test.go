package worktree_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	worktreerepo "github.com/usenorn/runner/internal/repository/worktree"
)

func settings() config.Snapshot {
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

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=norn",
		"GIT_AUTHOR_EMAIL=norn@example.com",
		"GIT_COMMITTER_NAME=norn",
		"GIT_COMMITTER_EMAIL=norn@example.com",
	)

	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}

	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func commit(t *testing.T, dir, name, body string) string {
	t.Helper()

	write(t, filepath.Join(dir, name), body)
	git(t, dir, "add", name)
	git(t, dir, "commit", "-q", "-m", "change "+name)

	return git(t, dir, "rev-parse", "HEAD")
}

func origin(t *testing.T) (string, string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed, so a real repository cannot be built")
	}

	base := t.TempDir()
	bare := filepath.Join(base, "origin.git")

	if out, err := exec.Command("git", "init", "--bare", "-q", "-b", "main", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	source := filepath.Join(base, "runner")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("create %s: %v", source, err)
	}

	git(t, source, "init", "-q", "-b", "main")
	commit(t, source, "a.txt", "one\n")
	git(t, source, "remote", "add", "origin", bare)
	git(t, source, "push", "-q", "origin", "main")
	git(t, source, "fetch", "-q", "origin")

	return source, bare
}

func maker(t *testing.T) repository.Worktree {
	t.Helper()

	return worktreerepo.New(settings(), results())
}

func TestAWorktreeStartsAtTheBaseCommitOnABranchOfItsOwn(t *testing.T) {
	source, _ := origin(t)
	into := filepath.Join(t.TempDir(), "workspace", "runner")
	worktrees := maker(t)

	base, err := worktrees.Resolve(context.Background(), source, "refs/remotes/origin/main")
	if err != nil {
		t.Fatalf("resolve the base of %s: %v", source, err)
	}

	if err := worktrees.Add(context.Background(), source, into, base); err != nil {
		t.Fatalf("add a worktree at %s: %v", into, err)
	}

	if err := worktrees.Branch(context.Background(), into, "norn/NORN-46/runner"); err != nil {
		t.Fatalf("branch the worktree: %v", err)
	}

	if got := git(t, into, "rev-parse", "HEAD"); got != base {
		t.Fatalf("the worktree sits at %s and the base is %s", got, base)
	}

	if got := git(t, into, "rev-parse", "--abbrev-ref", "HEAD"); got != "norn/NORN-46/runner" {
		t.Fatalf("the worktree is on %q", got)
	}

	if body, err := os.ReadFile(filepath.Join(into, "a.txt")); err != nil || string(body) != "one\n" {
		t.Fatalf("the worktree does not carry the repository's files: %q, %v", body, err)
	}
}

func TestARestartPicksUpTheBranchTheLastAttemptLeftBehind(t *testing.T) {
	source, _ := origin(t)
	worktrees := maker(t)
	base := git(t, source, "rev-parse", "HEAD")

	first := filepath.Join(t.TempDir(), "first")

	if err := worktrees.Add(context.Background(), source, first, base); err != nil {
		t.Fatalf("add the first worktree: %v", err)
	}

	if err := worktrees.Branch(context.Background(), first, "norn/NORN-46/runner"); err != nil {
		t.Fatalf("branch the first worktree: %v", err)
	}

	carried := commit(t, first, "b.txt", "work in progress\n")

	if err := worktrees.Remove(context.Background(), source, first); err != nil {
		t.Fatalf("remove the first worktree: %v", err)
	}

	second := filepath.Join(t.TempDir(), "second")

	if err := worktrees.Add(context.Background(), source, second, base); err != nil {
		t.Fatalf("add the second worktree: %v", err)
	}

	if err := worktrees.Branch(context.Background(), second, "norn/NORN-46/runner"); err != nil {
		t.Fatalf("branch the second worktree: %v", err)
	}

	if got := git(t, second, "rev-parse", "HEAD"); got != carried {
		t.Fatalf(
			"the restart sits at %s and the branch it was told to reuse ends at %s; a restart "+
				"after an interrupted run has to find the work the last attempt committed",
			got, carried,
		)
	}
}

func TestABranchAnotherWorktreeIsHoldingIsRefusedInWordsAPersonCanActOn(t *testing.T) {
	source, _ := origin(t)
	worktrees := maker(t)
	base := git(t, source, "rev-parse", "HEAD")

	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")

	for _, into := range []string{first, second} {
		if err := worktrees.Add(context.Background(), source, into, base); err != nil {
			t.Fatalf("add a worktree at %s: %v", into, err)
		}
	}

	if err := worktrees.Branch(context.Background(), first, "norn/NORN-46/runner"); err != nil {
		t.Fatalf("branch the first worktree: %v", err)
	}

	err := worktrees.Branch(context.Background(), second, "norn/NORN-46/runner")
	if !errors.Is(err, entity.ErrSnapshotWorktreeExists) {
		t.Fatalf(
			"a branch already checked out elsewhere failed with %v; two executions on one branch "+
				"would overwrite each other's work, so this has to be named, not guessed at",
			err,
		)
	}
}

func TestLocalWorkIsCarriedOntoTheSnapshotAsOneCommit(t *testing.T) {
	source, _ := origin(t)
	worktrees := maker(t)
	base := git(t, source, "rev-parse", "HEAD")

	write(t, filepath.Join(source, "a.txt"), "one and a half\n")
	write(t, filepath.Join(source, "new.txt"), "brand new\n")

	into := filepath.Join(t.TempDir(), "workspace")

	if err := worktrees.Add(context.Background(), source, into, base); err != nil {
		t.Fatalf("add a worktree: %v", err)
	}

	if err := worktrees.Branch(context.Background(), into, "norn/NORN-46/runner"); err != nil {
		t.Fatalf("branch the worktree: %v", err)
	}

	changed, err := worktrees.Changed(context.Background(), source)
	if err != nil || len(changed) != 1 || changed[0] != "a.txt" {
		t.Fatalf("the tracked change came back as %v, %v", changed, err)
	}

	untracked, err := worktrees.Untracked(context.Background(), source)
	if err != nil || len(untracked) != 1 || untracked[0] != "new.txt" {
		t.Fatalf("the untracked file came back as %v, %v", untracked, err)
	}

	patch, err := worktrees.Diff(context.Background(), source, changed)
	if err != nil {
		t.Fatalf("read the local changes: %v", err)
	}

	if err := worktrees.Apply(context.Background(), into, patch); err != nil {
		t.Fatalf("apply the local changes: %v", err)
	}

	write(t, filepath.Join(into, "new.txt"), "brand new\n")

	if err := worktrees.Stage(context.Background(), into, untracked); err != nil {
		t.Fatalf("stage the untracked file: %v", err)
	}

	sha, err := worktrees.Commit(context.Background(), into, entity.LocalChangesMessage(base))
	if err != nil {
		t.Fatalf("commit the local changes: %v", err)
	}

	if status := git(t, into, "status", "--porcelain"); status != "" {
		t.Fatalf(
			"the snapshot is dirty after carrying local work across:\n%s\nthe coding agent has to "+
				"start from a clean tree or its own diff becomes unreadable",
			status,
		)
	}

	if git(t, into, "rev-parse", "HEAD") != sha {
		t.Fatalf("the commit that was reported is not the one the snapshot is on")
	}

	if git(t, into, "log", "-1", "--format=%s") != entity.LocalChangesMessage(base) {
		t.Fatalf("the provisional commit does not say where the changes came from")
	}
}

func TestLocalWorkThatDoesNotApplyFailsWithGitsOwnWords(t *testing.T) {
	source, _ := origin(t)
	worktrees := maker(t)

	first := git(t, source, "rev-parse", "HEAD")

	commit(t, source, "a.txt", "two\n")
	write(t, filepath.Join(source, "a.txt"), "three\n")

	into := filepath.Join(t.TempDir(), "workspace")

	if err := worktrees.Add(context.Background(), source, into, first); err != nil {
		t.Fatalf("add a worktree at the older commit: %v", err)
	}

	changed, err := worktrees.Changed(context.Background(), source)
	if err != nil {
		t.Fatalf("read what changed: %v", err)
	}

	patch, err := worktrees.Diff(context.Background(), source, changed)
	if err != nil {
		t.Fatalf("read the local changes: %v", err)
	}

	err = worktrees.Apply(context.Background(), into, patch)
	if !errors.Is(err, entity.ErrSnapshotDirtyConflict) {
		t.Fatalf("a patch that does not apply failed with %v", err)
	}

	if !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf(
			"the refusal does not say which file could not be applied: %v; a half-applied tree "+
				"with a vague message is worse than no snapshot at all",
			err,
		)
	}

	if body, readErr := os.ReadFile(filepath.Join(into, "a.txt")); readErr != nil || string(body) != "one\n" {
		t.Fatalf("the worktree was left half-applied as %q, %v", body, readErr)
	}
}

func TestRemovingAWorktreeLeavesNoRegistrationBehind(t *testing.T) {
	source, _ := origin(t)
	worktrees := maker(t)
	base := git(t, source, "rev-parse", "HEAD")
	into := filepath.Join(t.TempDir(), "workspace")

	if err := worktrees.Add(context.Background(), source, into, base); err != nil {
		t.Fatalf("add a worktree: %v", err)
	}

	if lines := strings.Split(git(t, source, "worktree", "list"), "\n"); len(lines) != 2 {
		t.Fatalf("the repository lists %d worktrees before teardown", len(lines))
	}

	if err := worktrees.Remove(context.Background(), source, into); err != nil {
		t.Fatalf("remove the worktree: %v", err)
	}

	lines := strings.Split(git(t, source, "worktree", "list"), "\n")
	if len(lines) != 1 {
		t.Fatalf(
			"the repository still lists %d worktrees:\n%s\na registration left behind makes the "+
				"person's own repository complain about a folder that is gone",
			len(lines), strings.Join(lines, "\n"),
		)
	}

	if _, err := os.Stat(into); err == nil {
		t.Fatalf("%s is still on disk after it was removed", into)
	}
}

func TestFetchingBringsTheRemoteBranchForwardWithoutTouchingTheWorkingTree(t *testing.T) {
	source, bare := origin(t)
	worktrees := maker(t)

	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", bare, other)

	moved := commit(t, other, "a.txt", "moved on\n")
	git(t, other, "push", "-q", "origin", "main")

	before := git(t, source, "rev-parse", "HEAD")

	if err := worktrees.Fetch(context.Background(), source, "main"); err != nil {
		t.Fatalf("fetch main into %s: %v", source, err)
	}

	base, err := worktrees.Resolve(context.Background(), source, "refs/remotes/origin/main")
	if err != nil {
		t.Fatalf("resolve the fetched base: %v", err)
	}

	if base != moved {
		t.Fatalf("the base after fetching is %s and the remote is at %s", base, moved)
	}

	if git(t, source, "rev-parse", "HEAD") != before {
		t.Fatalf(
			"fetching moved the person's own checkout; a snapshot reads their repository and " +
				"never rearranges it",
		)
	}
}

func TestCloneModeProducesTheSameStartingPointAsAWorktree(t *testing.T) {
	source, _ := origin(t)
	worktrees := maker(t)
	base := git(t, source, "rev-parse", "HEAD")
	into := filepath.Join(t.TempDir(), "workspace", "runner")

	if err := worktrees.Clone(context.Background(), source, into, base); err != nil {
		t.Fatalf("clone %s: %v", source, err)
	}

	if err := worktrees.Branch(context.Background(), into, "norn/NORN-46/runner"); err != nil {
		t.Fatalf("branch the clone: %v", err)
	}

	if got := git(t, into, "rev-parse", "HEAD"); got != base {
		t.Fatalf("the clone sits at %s and the base is %s", got, base)
	}

	if body, err := os.ReadFile(filepath.Join(into, "a.txt")); err != nil || string(body) != "one\n" {
		t.Fatalf("the clone does not carry the repository's files: %q, %v", body, err)
	}
}

func TestPruningStaleWorktreesLetsANewExecutionTakeTheBranch(t *testing.T) {
	source, _ := origin(t)
	worktrees := maker(t)
	base := git(t, source, "rev-parse", "HEAD")

	left := filepath.Join(t.TempDir(), "left")

	if err := worktrees.Add(context.Background(), source, left, base); err != nil {
		t.Fatalf("add a worktree at %s: %v", left, err)
	}

	if err := worktrees.Branch(context.Background(), left, "norn/NORN-46/runner"); err != nil {
		t.Fatalf("branch the worktree: %v", err)
	}

	if err := os.RemoveAll(left); err != nil {
		t.Fatalf("remove the worktree directory: %v", err)
	}

	into := filepath.Join(t.TempDir(), "workspace")

	if err := worktrees.Add(context.Background(), source, into, base); err != nil {
		t.Fatalf("add a new worktree: %v", err)
	}

	if err := worktrees.Branch(context.Background(), into, "norn/NORN-46/runner"); err != nil {
		t.Fatalf(
			"a stale worktree registration blocked a fresh one even though the directory is "+
				"gone: %v",
			err,
		)
	}

	if got := git(t, into, "rev-parse", "--abbrev-ref", "HEAD"); got != "norn/NORN-46/runner" {
		t.Fatalf("the worktree is on %q", got)
	}
}

func results() config.Results {
	return config.Results{
		CreatePRs:    config.PullRequestsAuto,
		PushTimeout:  60 * time.Second,
		ForgeTimeout: 30 * time.Second,
		MaxDiffBytes: 3 << 20,
	}
}
