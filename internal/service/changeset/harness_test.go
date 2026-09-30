package changeset_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	forgerepo "github.com/usenorn/runner/internal/repository/forge"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	worktreerepo "github.com/usenorn/runner/internal/repository/worktree"
	"github.com/usenorn/runner/internal/service"
	changesetsvc "github.com/usenorn/runner/internal/service/changeset"
	uploadsvc "github.com/usenorn/runner/internal/service/upload"
)

const executionID = "exec-01ABC"

type harness struct {
	remoteTips map[string]string
	pushed     []entity.Push
	runs       repository.Run
	spool      repository.Spool
	worktrees  *worktreerepo.MockWorktree
	forges     *forgerepo.MockForge
	uploads    *uploadsvc.MockUploads
	service    service.ChangeSets

	execution entity.Execution
	snapshot  entity.Snapshot
}

func newHarness(t *testing.T, results config.Results) *harness {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	controller := gomock.NewController(t)

	h := &harness{
		remoteTips: map[string]string{},
		runs:       runrepo.New(dir),
		spool:      spoolrepo.New(dir),
		worktrees:  worktreerepo.NewMockWorktree(controller),
		forges:     forgerepo.NewMockForge(controller),
		uploads:    uploadsvc.NewMockUploads(controller),
	}

	if _, err := h.runs.Prepare(context.Background(), executionID); err != nil {
		t.Fatalf("make a run directory: %v", err)
	}

	h.execution = entity.Execution{
		ID:       executionID,
		IssueKey: "NORN-54",
		Title:    "Finalising",
		Attempt:  1,
	}

	h.snapshot = entity.Snapshot{
		Name:     executionID,
		IssueKey: "NORN-54",
		Repositories: []entity.SnapshotRepository{
			repositoryAt("backend", "base-backend"),
			repositoryAt("frontend", "base-frontend"),
		},
	}

	h.service = changesetsvc.New(
		h.runs, h.spool, h.worktrees, h.forges, h.uploads, results,
	)

	return h
}

func repositoryAt(name, base string) entity.SnapshotRepository {
	return entity.SnapshotRepository{
		Name:    name,
		RelPath: name,
		Mode:    entity.GitModeWorktree,
		Source:  "/codebase/" + name,
		Path:    "/run/workspace/" + name,
		BaseSHA: base,
		Branch:  entity.BranchFor("NORN-54", name, 1),
	}
}

func defaults() config.Results {
	return config.Results{
		CreatePRs:    config.PullRequestsAuto,
		PushTimeout:  time.Second,
		ForgeTimeout: time.Second,
		MaxDiffBytes: 1 << 20,
	}
}

func (h *harness) changed(commits int, stat entity.Diffstat) {
	h.worktrees.EXPECT().
		Commits(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(commits, nil).
		AnyTimes()

	h.worktrees.EXPECT().
		Head(gomock.Any(), gomock.Any()).
		Return("head-sha", nil).
		AnyTimes()

	h.worktrees.EXPECT().
		Diffstat(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(stat, nil).
		AnyTimes()

	h.worktrees.EXPECT().
		History(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return([]entity.Commit{{SHA: "head-sha", Subject: "add a median helper"}}, nil).
		AnyTimes()

	h.worktrees.EXPECT().
		Patch(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return([]byte("diff --git a/a b/a\n+one\n"), nil).
		AnyTimes()
}

func (h *harness) remotes() {
	for index := range h.snapshot.Repositories {
		h.snapshot.Repositories[index].Remote = "git@github.com:usenorn/runner.git"
		h.snapshot.Repositories[index].Lease = entity.Lease{Known: true}
	}

	h.worktrees.EXPECT().
		RemoteTip(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, branch string) (string, error) {
			return h.remoteTips[branch], nil
		}).
		AnyTimes()
}

func (h *harness) pushes() {
	h.remotes()

	h.worktrees.EXPECT().
		Push(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, push entity.Push) error {
			h.pushed = append(h.pushed, push)
			h.remoteTips[push.Branch] = push.SHA

			return nil
		}).
		AnyTimes()
}

func (h *harness) keeps(id string) {
	h.uploads.EXPECT().
		Attach(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.ArtifactReceipt{ID: id}, nil).
		AnyTimes()
}

func (h *harness) noForge() {
	h.forges.EXPECT().
		Available(gomock.Any(), gomock.Any()).
		Return(entity.ForgeKind(""), false).
		AnyTimes()
}

func (h *harness) collect(t *testing.T, summary string) entity.ChangeSet {
	t.Helper()

	changes, err := h.service.Collect(
		context.Background(), h.execution, h.snapshot, entity.Completion{Summary: summary},
		entity.ReviewPass{Revision: 1},
	)
	if err != nil {
		t.Fatalf("collect what the run changed: %v", err)
	}

	return changes
}

func (h *harness) publish(t *testing.T, summary string) (entity.ChangeSet, entity.Publication) {
	t.Helper()

	changes := h.collect(t, summary)

	return changes, h.republish(t, entity.ReviewOf(1, summary, h.snapshot, changes))
}

func (h *harness) republish(t *testing.T, review entity.Review) entity.Publication {
	t.Helper()

	publication, err := h.service.Publish(context.Background(), h.execution, review)
	if err != nil {
		t.Fatalf("publish what the run changed: %v", err)
	}

	return publication
}

func (h *harness) sent(t *testing.T, kind channelv1.MessageType) []channelv1.Message {
	t.Helper()

	held, err := h.spool.Head(context.Background(), 100)
	if err != nil {
		t.Fatalf("read the spool: %v", err)
	}

	found := make([]channelv1.Message, 0, len(held))

	for _, message := range held {
		if message.Type == kind {
			found = append(found, message)
		}
	}

	return found
}

func decodeInto[T any](t *testing.T, message channelv1.Message) T {
	t.Helper()

	var held T

	if err := json.Unmarshal(message.Payload, &held); err != nil {
		t.Fatalf("decode a %s: %v", message.Type, err)
	}

	return held
}

func (h *harness) noted(t *testing.T, fragment string) bool {
	t.Helper()

	timeline, err := h.runs.Timeline(context.Background(), h.execution.ID)
	if err != nil {
		t.Fatalf("read the run's timeline: %v", err)
	}

	for _, entry := range timeline {
		if strings.Contains(entry.Reason, fragment) {
			return true
		}
	}

	return false
}
