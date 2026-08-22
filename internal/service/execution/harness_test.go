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
	diskrepo "github.com/usenorn/runner/internal/repository/disk"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	settingsrepo "github.com/usenorn/runner/internal/repository/settings"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	"github.com/usenorn/runner/internal/service"
	executionsvc "github.com/usenorn/runner/internal/service/execution"
	snapshotsvc "github.com/usenorn/runner/internal/service/snapshot"
)

const patience = 5 * time.Second

type harness struct {
	dir         *statedir.Dir
	runs        repository.Run
	spool       repository.Spool
	disks       *diskrepo.MockDisk
	settings    *settingsrepo.MockSettings
	inventories *inventoryrepo.MockInventory
	snapshots   *snapshotsvc.MockSnapshots
	service     service.Executions

	free      int64
	freeErr   error
	connected []entity.Codebase
	planFile  string

	mu       sync.Mutex
	takeErr  error
	linger   time.Duration
	taken    []service.TakeRequest
	released []string
}

func newHarness(t *testing.T, capacity int, watermark int64) *harness {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	return build(t, dir, capacity, watermark, 100<<30)
}

func newHarnessOver(t *testing.T, first *harness, capacity int, watermark int64) *harness {
	t.Helper()

	return build(t, first.dir, capacity, watermark, first.free)
}

func build(t *testing.T, dir *statedir.Dir, capacity int, watermark, free int64) *harness {
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
		free:        free,
		connected:   []entity.Codebase{connected("/codebase")},
	}

	h.expect()

	h.service = executionsvc.New(
		h.runs,
		h.spool,
		h.disks,
		h.settings,
		h.inventories,
		h.snapshots,
		dir,
		config.Runner{Capacity: capacity},
		config.App{Version: "1.4.0"},
		config.Scheduler{MinFreeDisk: watermark},
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
			Name:   "runner",
			Mode:   entity.GitModeWorktree,
			Branch: branchFor(request),
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

	go func() {
		defer close(done)

		h.service.Run(ctx)
	}()

	return func() {
		stop()
		<-done
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
