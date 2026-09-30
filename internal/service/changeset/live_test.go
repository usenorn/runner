package changeset_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	worktreerepo "github.com/usenorn/runner/internal/repository/worktree"
	changesetsvc "github.com/usenorn/runner/internal/service/changeset"
)

type quietForge struct{}

func (quietForge) Available(context.Context, string) (entity.ForgeKind, bool) { return "", false }

func (quietForge) Existing(context.Context, string, string) (string, error) { return "", nil }

func (quietForge) Open(context.Context, string, entity.PullRequest) (string, error) {
	return "", entity.ErrForgeAbsent
}

type keptDiff struct {
	body []byte
}

func (k *keptDiff) Attach(
	_ context.Context, _, _ string, body []byte,
) (entity.ArtifactReceipt, error) {
	k.body = body

	return entity.ArtifactReceipt{ID: "f8b0a1c2-0000-4000-8000-000000000001"}, nil
}

func (k *keptDiff) Publish(
	context.Context, string, entity.Artifact,
) (entity.ArtifactReceipt, error) {
	return entity.ArtifactReceipt{}, nil
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()

	out, err := runIn(dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}

	return out
}

func runIn(dir string, args ...string) (string, error) {
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

	return strings.TrimSpace(string(out)), err
}

func realRepository(t *testing.T) (string, string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed, so a real repository cannot be built")
	}

	base := t.TempDir()
	bare := filepath.Join(base, "origin.git")

	if out, err := exec.Command(
		"git", "init", "--bare", "-q", "-b", "main", bare,
	).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	source := filepath.Join(base, "ledger")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("create %s: %v", source, err)
	}

	run(t, source, "init", "-q", "-b", "main")

	if err := os.WriteFile(
		filepath.Join(source, "median.py"), []byte("def mean():\n"), 0o644,
	); err != nil {
		t.Fatalf("write a file: %v", err)
	}

	run(t, source, "add", "-A")
	run(t, source, "commit", "-q", "-m", "add mean")
	run(t, source, "remote", "add", "origin", bare)
	run(t, source, "push", "-q", "origin", "main")

	return source, bare
}

func TestWhatARunChangedIsCollectedForReviewAndPushedOnlyOnceApproved(t *testing.T) {
	source, bare := realRepository(t)

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	runs := runrepo.New(dir)
	spool := spoolrepo.New(dir)
	worktrees := worktreerepo.New(config.Snapshot{
		GitMode: "worktree", Base: "origin/default", LocalChanges: "exclude",
		FetchTimeout: defaults().PushTimeout, GitTimeout: defaults().PushTimeout,
		MaxSharedBytes: 1 << 30,
	}, defaults())

	ctx := context.Background()

	if _, err := runs.Prepare(ctx, executionID); err != nil {
		t.Fatalf("make a run directory: %v", err)
	}

	base := run(t, source, "rev-parse", "HEAD")
	workspace := filepath.Join(dir.Run(executionID), entity.RunWorkspaceDir, "ledger")
	branch := entity.BranchFor("NORN-54", "ledger", 1)

	if err := worktrees.Add(ctx, source, workspace, base); err != nil {
		t.Fatalf("make the run a workspace: %v", err)
	}

	if err := worktrees.Branch(ctx, workspace, branch); err != nil {
		t.Fatalf("put the workspace on a branch: %v", err)
	}

	snapshot := entity.Snapshot{
		Name:     executionID,
		IssueKey: "NORN-54",
		Repositories: []entity.SnapshotRepository{{
			Name: "ledger", RelPath: "ledger", Mode: entity.GitModeWorktree,
			Source: source, Path: workspace, BaseSHA: base, Branch: branch,
			Remote: bare, Lease: entity.Lease{Known: true},
		}},
	}

	if err := os.WriteFile(
		filepath.Join(workspace, "median.py"), []byte("def mean():\n\ndef median():\n"), 0o644,
	); err != nil {
		t.Fatalf("write what the coding agent wrote: %v", err)
	}

	left, err := changesetsvc.New(
		runs, spool, worktrees, quietForge{}, &keptDiff{}, defaults(),
	).Uncommitted(ctx, snapshot)
	if err != nil {
		t.Fatalf("look for uncommitted work: %v", err)
	}

	if len(left) != 1 || len(left[0].Files) != 1 {
		t.Fatalf(
			"an edit the coding agent never committed reads as %+v; it would be thrown away with "+
				"the workspace and never appear in the diff a person reviews",
			left,
		)
	}

	run(t, workspace, "add", "-A")
	run(t, workspace, "commit", "-q", "-m", "add median")

	pushOnly := defaults()
	pushOnly.CreatePRs = config.PullRequestsPushOnly

	kept := &keptDiff{}
	changesets := changesetsvc.New(runs, spool, worktrees, quietForge{}, kept, pushOnly)

	if left, err = changesets.Uncommitted(ctx, snapshot); err != nil || len(left) != 0 {
		t.Fatalf("a committed workspace still reads as dirty: %+v (%v)", left, err)
	}

	execution := entity.Execution{ID: executionID, IssueKey: "NORN-54", Title: "Finalising"}

	collected, err := changesets.Collect(
		ctx, execution, snapshot, entity.Completion{Summary: "added a median helper"},
		entity.ReviewPass{Revision: 1},
	)
	if err != nil {
		t.Fatalf("collect what the run changed: %v", err)
	}

	if history := collected.Repositories[0].History; len(history) != collected.Repositories[0].Commits ||
		history[0].SHA != collected.Repositories[0].HeadSHA || history[0].Subject == "" {
		t.Fatalf("the reviewed commits read %+v, want every commit newest first with its subject", history)
	}

	if _, err := runIn(bare, "rev-parse", "--verify", "--quiet", branch); err == nil {
		t.Fatal(
			"the branch reached the remote while its changes were still waiting for review; " +
				"a person has to approve the work before anything leaves the machine",
		)
	}

	reviewed := collected.Repositories[0]

	if reviewed.Commits != 1 || reviewed.Diffstat.Additions != 2 || reviewed.Diffstat.Files != 1 {
		t.Fatalf(
			"the run reads as %+v against a repository it added two lines to in one file",
			reviewed,
		)
	}

	if reviewed.HeadSHA != run(t, workspace, "rev-parse", "HEAD") {
		t.Fatalf("the run reports head %q", reviewed.HeadSHA)
	}

	if err := os.WriteFile(filepath.Join(workspace, "late.py"), []byte("late = 1\n"), 0o644); err != nil {
		t.Fatalf("write what a preview service wrote after review: %v", err)
	}

	run(t, workspace, "add", "-A")
	run(t, workspace, "commit", "-q", "-m", "committed after the review")

	review := entity.ReviewOf(1, "added a median helper", snapshot, collected)

	publication, err := changesets.Publish(ctx, execution, review)
	if err != nil {
		t.Fatalf("push what the run changed: %v", err)
	}

	if !publication.Complete() {
		t.Fatalf("a push the remote accepted reads as %+v", publication)
	}

	pushed := run(t, bare, "rev-parse", branch)
	if pushed != reviewed.HeadSHA {
		t.Fatalf(
			"the remote holds %q and the reviewed commit is %q; a commit made after review "+
				"reached the remote although nobody approved it",
			pushed, reviewed.HeadSHA,
		)
	}

	again, err := changesets.Publish(ctx, execution, review)
	if err != nil || !again.Complete() || again.Attempt != 2 {
		t.Fatalf("publishing the same approval again came back %+v (%v)", again, err)
	}

	if len(kept.body) < 2 || kept.body[0] != 0x1f || kept.body[1] != 0x8b {
		t.Fatalf(
			"the diff was kept as %d raw bytes; norn holds a few megabytes per artifact and a "+
				"patch compresses many times over",
			len(kept.body),
		)
	}

	result := decodeInto[channelv1.Result](t, lastOf(t, spool, channelv1.ExecutionResult))

	if result.Summary != "added a median helper" || len(result.ChangeSet.Repos) != 1 {
		t.Fatalf("norn was told %+v", result)
	}

	if result.ChangeSet.Repos[0].Diff != "f8b0a1c2-0000-4000-8000-000000000001" {
		t.Fatalf("the diff artifact went out as %q", result.ChangeSet.Repos[0].Diff)
	}

	if result.ChangeSet.Repos[0].Branch != branch {
		t.Fatalf("the branch went out as %q", result.ChangeSet.Repos[0].Branch)
	}
}

func lastOf(
	t *testing.T,
	spool repository.Spool,
	kind channelv1.MessageType,
) channelv1.Message {
	t.Helper()

	waiting, err := spool.Head(context.Background(), 100)
	if err != nil {
		t.Fatalf("read the spool: %v", err)
	}

	found := channelv1.Message{}

	for _, message := range waiting {
		if message.Type == kind {
			found = message
		}
	}

	if found.ID == "" {
		t.Fatalf("nothing in the spool is a %s", kind)
	}

	return found
}
