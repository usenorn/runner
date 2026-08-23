package execution_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
	diskrepo "github.com/usenorn/runner/internal/repository/disk"
	forgerepo "github.com/usenorn/runner/internal/repository/forge"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	runtokenrepo "github.com/usenorn/runner/internal/repository/runtoken"
	schedulingrepo "github.com/usenorn/runner/internal/repository/scheduling"
	settingsrepo "github.com/usenorn/runner/internal/repository/settings"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	uploadrepo "github.com/usenorn/runner/internal/repository/upload"
	worktreerepo "github.com/usenorn/runner/internal/repository/worktree"
	"github.com/usenorn/runner/internal/service"
	changesetsvc "github.com/usenorn/runner/internal/service/changeset"
	executionsvc "github.com/usenorn/runner/internal/service/execution"
	previewsvc "github.com/usenorn/runner/internal/service/preview"
	questionsvc "github.com/usenorn/runner/internal/service/question"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
	snapshotsvc "github.com/usenorn/runner/internal/service/snapshot"
	supervisorsvc "github.com/usenorn/runner/internal/service/supervisor"
	uploadsvc "github.com/usenorn/runner/internal/service/upload"
)

const patience = 5 * time.Second

type harness struct {
	dir         *statedir.Dir
	runs        repository.Run
	previews    service.Previews
	tokens      repository.RunToken
	spool       repository.Spool
	disks       *diskrepo.MockDisk
	settings    *settingsrepo.MockSettings
	inventories *inventoryrepo.MockInventory
	snapshots   *snapshotsvc.MockSnapshots
	services    *supervisorsvc.MockServices
	drivers     *driverStub
	posts       *uploadrepo.MockUpload
	dashboard   *dashboardrepo.MockDashboard
	sessions    *sessionsvc.MockSessions
	worktrees   *worktreerepo.MockWorktree
	forges      *forgerepo.MockForge
	changesets  service.ChangeSets
	uploads     service.Uploads
	questions   service.Questions
	service     service.Executions

	free      int64
	freeErr   error
	connected []entity.Codebase
	planFile  string
	telemetry entity.TelemetryMode
	profile   config.Profile

	dirty    map[string][]string
	commits  int
	stat     entity.Diffstat
	patch    []byte
	remote   string
	remoteEr error
	pushErr  error
	forge    bool
	opened   string
	openErr  error
	existing string

	mu          sync.Mutex
	pushed      []string
	requested   []entity.PullRequest
	takeErr     error
	linger      time.Duration
	taken       []service.TakeRequest
	released    []string
	transcripts []entity.TranscriptBatch
	logs        []entity.LogBatch
}

func newHarness(t *testing.T, capacity int, watermark int64) *harness {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	return build(t, dir, capacity, watermark, 100<<30, keeping(), config.ProfileStandard)
}

func newHarnessUnder(t *testing.T, profile config.Profile) *harness {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	return build(t, dir, 2, 0, 100<<30, keeping(), profile)
}

func newHarnessKeeping(t *testing.T, retention config.Retention) *harness {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	return build(t, dir, 2, 0, 100<<30, retention, config.ProfileStandard)
}

func keeping() config.Retention {
	return config.Retention{
		WorkspaceAfterDone: 30 * time.Minute,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      time.Hour,
	}
}

func newHarnessOver(t *testing.T, first *harness, capacity int, watermark int64) *harness {
	t.Helper()

	return build(t, first.dir, capacity, watermark, first.free, keeping(), config.ProfileStandard)
}

func newHarnessOverKeeping(
	t *testing.T,
	first *harness,
	retention config.Retention,
) *harness {
	t.Helper()

	return build(t, first.dir, 2, 0, first.free, retention, config.ProfileStandard)
}

func build(
	t *testing.T,
	dir *statedir.Dir,
	capacity int,
	watermark, free int64,
	retention config.Retention,
	profile config.Profile,
) *harness {
	t.Helper()

	controller := gomock.NewController(t)

	h := &harness{
		dir:         dir,
		runs:        runrepo.New(dir),
		spool:       spoolrepo.New(dir),
		disks:       diskrepo.NewMockDisk(controller),
		settings:    settingsrepo.NewMockSettings(controller),
		inventories: inventoryrepo.NewMockInventory(controller),
		snapshots:   snapshotsvc.NewMockSnapshots(controller),
		services:    supervisorsvc.NewMockServices(controller),
		drivers:     newDriverStub(),
		posts:       uploadrepo.NewMockUpload(controller),
		dashboard:   dashboardrepo.NewMockDashboard(controller),
		sessions:    sessionsvc.NewMockSessions(controller),
		worktrees:   worktreerepo.NewMockWorktree(controller),
		forges:      forgerepo.NewMockForge(controller),
		free:        free,
		connected:   []entity.Codebase{connected("/codebase")},
		telemetry:   entity.TelemetryFull,
		profile:     profile,
		dirty:       map[string][]string{},
		remote:      "git@github.com:usenorn/runner.git",
		patch:       []byte("diff --git a/a b/a\n"),
	}

	h.expect()

	h.uploads = uploadsvc.New(h.posts, h.runs, h.dashboard, h.sessions, config.Upload{
		Enabled:          true,
		Batch:            2,
		Flush:            10 * time.Millisecond,
		MaxChunkBytes:    1 << 20,
		MaxPending:       8,
		MaxArtifactBytes: 1 << 20,
	})

	h.questions = questionsvc.New(
		h.runs, h.spool, config.Questions{SoftWait: 20 * time.Millisecond, MaxWait: time.Second},
	)

	h.previews = previewsvc.New(h.runs, h.spool)
	h.changesets = changesetsvc.New(
		h.runs, h.spool, h.worktrees, h.forges, h.uploads, results(),
	)
	h.tokens = runtokenrepo.New()

	h.service = executionsvc.New(
		h.runs,
		h.spool,
		h.disks,
		schedulingrepo.New(dir),
		h.settings,
		h.inventories,
		h.snapshots,
		h.services,
		h.uploads,
		h.questions,
		h.previews,
		h.changesets,
		h.tokens,
		h.drivers,
		dir,
		config.Runner{Capacity: capacity, Retention: retention},
		config.App{Version: "1.4.0"},
		config.Scheduler{MinFreeDisk: watermark},
		config.Driver{
			Profile:        h.profile,
			ProbeTimeout:   time.Second,
			SessionTimeout: time.Minute,
			StopGrace:      10 * time.Millisecond,
			ResumeAttempts: 1,
		},
	)

	return h
}

func connected(root string) entity.Codebase {
	return entity.Codebase{
		Name:     "codebase",
		RootPath: root,
		Confirmed: entity.Inventory{
			RootPath:     root,
			Repositories: []entity.Repository{{Name: "runner", RelPath: "runner"}},
			Runtimes:     []entity.Runtime{entity.RuntimeProcess},
			Tools:        []entity.Tool{{Name: "claude", Version: "2.0.1"}},
		},
	}
}

func (h *harness) expect() {
	h.disks.EXPECT().
		Free(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (int64, error) { return h.free, h.freeErr }).
		AnyTimes()

	h.inventories.EXPECT().
		List(gomock.Any()).
		DoAndReturn(func(context.Context) ([]entity.Codebase, error) { return h.connected, nil }).
		AnyTimes()

	h.settings.EXPECT().
		Plan(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (string, error) { return h.planFile, nil }).
		AnyTimes()

	h.snapshots.EXPECT().
		Take(gomock.Any(), gomock.Any()).
		DoAndReturn(h.take).
		AnyTimes()

	h.snapshots.EXPECT().
		Release(gomock.Any(), gomock.Any()).
		DoAndReturn(h.release).
		AnyTimes()

	h.services.EXPECT().
		Release(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	h.sessions.EXPECT().
		Access(gomock.Any()).
		Return("access-token", nil).
		AnyTimes()

	h.dashboard.EXPECT().
		Telemetry(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (entity.TelemetryMode, error) {
			return h.telemetry, nil
		}).
		AnyTimes()

	h.posts.EXPECT().
		Cursors(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil).
		AnyTimes()

	h.posts.EXPECT().
		AppendTranscript(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(h.appendTranscript).
		AnyTimes()

	h.posts.EXPECT().
		AppendLogs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(h.appendLogs).
		AnyTimes()

	h.expectGit()
	h.expectForge()
}

func (h *harness) expectGit() {
	h.worktrees.EXPECT().
		Changed(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, path string) ([]string, error) {
			return h.left(path), nil
		}).
		AnyTimes()

	h.worktrees.EXPECT().
		Untracked(gomock.Any(), gomock.Any()).
		Return(nil, nil).
		AnyTimes()

	h.worktrees.EXPECT().
		Commits(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, string) (int, error) { return h.commits, nil }).
		AnyTimes()

	h.worktrees.EXPECT().
		Head(gomock.Any(), gomock.Any()).
		Return("head-sha", nil).
		AnyTimes()

	h.worktrees.EXPECT().
		Diffstat(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, string) (entity.Diffstat, error) {
			return h.stat, nil
		}).
		AnyTimes()

	h.worktrees.EXPECT().
		Patch(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, string) ([]byte, error) { return h.patch, nil }).
		AnyTimes()

	h.worktrees.EXPECT().
		Remote(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (string, error) {
			return h.remote, h.remoteEr
		}).
		AnyTimes()

	h.worktrees.EXPECT().
		Push(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, branch string) error {
			if h.pushErr != nil {
				return h.pushErr
			}

			h.mu.Lock()
			h.pushed = append(h.pushed, branch)
			h.mu.Unlock()

			return nil
		}).
		AnyTimes()
}

func (h *harness) expectForge() {
	h.forges.EXPECT().
		Available(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (entity.ForgeKind, bool) {
			if !h.forge {
				return "", false
			}

			return entity.ForgeGitHub, true
		}).
		AnyTimes()

	h.forges.EXPECT().
		Existing(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, string) (string, error) {
			return h.existing, nil
		}).
		AnyTimes()

	h.forges.EXPECT().
		Open(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context, _ string, request entity.PullRequest,
		) (string, error) {
			h.mu.Lock()
			h.requested = append(h.requested, request)
			h.mu.Unlock()

			return h.opened, h.openErr
		}).
		AnyTimes()
}

func (h *harness) left(path string) []string {
	for repository, files := range h.dirty {
		if strings.HasSuffix(path, repository) {
			return files
		}
	}

	return nil
}

func (h *harness) appendTranscript(
	_ context.Context,
	_ string,
	_ string,
	batch entity.TranscriptBatch,
) (entity.UploadReceipt, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.transcripts = append(h.transcripts, batch)

	return entity.UploadReceipt{Stream: entity.StreamTranscript, Sequence: batch.Sequence}, nil
}

func (h *harness) appendLogs(
	_ context.Context,
	_ string,
	_ string,
	batch entity.LogBatch,
) (entity.UploadReceipt, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logs = append(h.logs, batch)

	return entity.UploadReceipt{Stream: entity.StreamLogs, Sequence: batch.Sequence}, nil
}

func (h *harness) sent() []entity.DriverEvent {
	h.mu.Lock()
	defer h.mu.Unlock()

	events := []entity.DriverEvent{}

	for _, batch := range h.transcripts {
		events = append(events, batch.Entries...)
	}

	return events
}

func (h *harness) logged() []entity.LogLine {
	h.mu.Lock()
	defer h.mu.Unlock()

	lines := []entity.LogLine{}

	for _, batch := range h.logs {
		lines = append(lines, batch.Entries...)
	}

	return lines
}

func (h *harness) take(
	ctx context.Context,
	request service.TakeRequest,
) (entity.Snapshot, error) {
	h.mu.Lock()
	h.taken = append(h.taken, request)
	linger, failing := h.linger, h.takeErr
	h.mu.Unlock()

	if linger > 0 {
		select {
		case <-ctx.Done():
			return entity.Snapshot{}, ctx.Err()
		case <-time.After(linger):
		}
	}

	if failing != nil {
		return entity.Snapshot{}, failing
	}

	if _, err := h.runs.Open(ctx, request.Run); err != nil {
		return entity.Snapshot{}, err
	}

	snapshot := entity.Snapshot{
		Name:      request.Run,
		IssueKey:  request.IssueKey,
		Attempt:   request.Attempt,
		Workspace: filepath.Join(h.dir.Run(request.Run), entity.RunWorkspaceDir),
		Repositories: []entity.SnapshotRepository{{
			Name:    "runner",
			RelPath: "runner",
			Mode:    entity.GitModeWorktree,
			Source:  filepath.Join("/codebase", "runner"),
			Path:    filepath.Join(h.dir.Run(request.Run), entity.RunWorkspaceDir, "runner"),
			BaseSHA: "base-sha",
			Branch:  branchFor(request),
		}},
		TakenAt: time.Now().UTC(),
	}

	return snapshot, h.runs.Save(ctx, snapshot)
}

func branchFor(request service.TakeRequest) string {
	if reused := request.Branches["runner"]; reused != "" {
		return reused
	}

	return entity.BranchFor(request.IssueKey, "runner", request.Attempt)
}

func (h *harness) release(ctx context.Context, name string) error {
	h.mu.Lock()
	h.released = append(h.released, name)
	h.mu.Unlock()

	return h.runs.Prune(ctx, name)
}

func (h *harness) requests() []service.TakeRequest {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]service.TakeRequest(nil), h.taken...)
}

func (h *harness) start(t *testing.T) func() {
	t.Helper()

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})

	sending := make(chan struct{})

	go func() {
		defer close(sending)

		h.uploads.Run(ctx)
	}()

	go func() {
		defer close(done)

		h.service.Run(ctx)
	}()

	return func() {
		stop()
		<-done
		<-sending
	}
}

func (h *harness) await(t *testing.T, what string, until func() bool) {
	t.Helper()

	deadline := time.Now().Add(patience)

	for time.Now().Before(deadline) {
		if until() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("%s, and it never did", what)
}

func (h *harness) awaitState(
	t *testing.T,
	executionID string,
	wanted entity.ExecutionState,
) {
	t.Helper()

	h.await(t, "waited for "+executionID+" to reach "+string(wanted), func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(wanted) {
				return true
			}
		}

		return false
	})
}

func (h *harness) awaitReview(t *testing.T, executionID string) {
	t.Helper()

	h.awaitState(t, executionID, channelv1.StateAwaitingReview)
}

func (h *harness) awaitNote(t *testing.T, wanted string) channelv1.Entry {
	t.Helper()

	var found channelv1.Entry

	h.await(t, "waited for the timeline to say "+wanted, func() bool {
		for _, message := range h.spooled(t) {
			if message.Type != channelv1.ExecutionEvent {
				continue
			}

			entry := decodeInto[channelv1.Entry](t, message)

			if strings.Contains(entry.Reason, wanted) {
				found = entry

				return true
			}
		}

		return false
	})

	return found
}

func (h *harness) reports(t *testing.T) []channelv1.Report {
	t.Helper()

	reported := []channelv1.Report{}

	for _, message := range h.spooled(t) {
		if message.Type == channelv1.ExecutionStateReport {
			reported = append(reported, decodeInto[channelv1.Report](t, message))
		}
	}

	return reported
}

func (h *harness) offer(id string) channelv1.Offer {
	return channelv1.Offer{
		ExecutionID: id,
		Reference:   "NORN-47",
		Attempt:     1,
		WorkspaceID: "01WORKSPACE",
		Issue:       channelv1.Issue{Reference: "NORN-47", Title: "Execution lifecycle"},
		Params:      channelv1.Params{Tool: "claude"},
	}
}

func (h *harness) spooled(t *testing.T) []channelv1.Message {
	t.Helper()

	waiting, err := h.spool.Head(context.Background(), 0)
	if err != nil {
		t.Fatalf("read the spool: %v", err)
	}

	return waiting
}

func (h *harness) sentOf(t *testing.T, kind channelv1.MessageType) []channelv1.Message {
	t.Helper()

	found := []channelv1.Message{}

	for _, message := range h.spooled(t) {
		if message.Type == kind {
			found = append(found, message)
		}
	}

	return found
}

func (h *harness) only(t *testing.T, kind channelv1.MessageType) channelv1.Message {
	t.Helper()

	waiting := h.spooled(t)

	for _, message := range waiting {
		if message.Type == kind {
			return message
		}
	}

	t.Fatalf("nothing in the spool is a %s; it holds %s", kind, kinds(waiting))

	return channelv1.Message{}
}

func kinds(waiting []channelv1.Message) string {
	held := ""

	for _, message := range waiting {
		held += string(message.Type) + " "
	}

	if held == "" {
		return "nothing"
	}

	return held
}

func decodeInto[T any](t *testing.T, message channelv1.Message) T {
	t.Helper()

	var held T

	if err := json.Unmarshal(message.Payload, &held); err != nil {
		t.Fatalf("read a %s: %v", message.Type, err)
	}

	return held
}

func started() channelv1.Start {
	lease := time.Now().UTC().Add(time.Minute)

	return channelv1.Start{ExecutionID: "exec-01ABC", LeaseExpiresAt: &lease}
}

func results() config.Results {
	return config.Results{
		CreatePRs:    config.PullRequestsAuto,
		Attribution:  config.AttributionNone,
		PushTimeout:  time.Second,
		ForgeTimeout: time.Second,
		MaxDiffBytes: 1 << 20,
	}
}
