package execution_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	diskrepo "github.com/usenorn/runner/internal/repository/disk"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	"github.com/usenorn/runner/internal/service"
	executionsvc "github.com/usenorn/runner/internal/service/execution"
)

type harness struct {
	dir     *statedir.Dir
	runs    repository.Run
	spool   repository.Spool
	disks   *diskrepo.MockDisk
	service service.Executions
	free    int64
	freeErr error
}

func newHarness(t *testing.T, capacity int, watermark int64) *harness {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	h := &harness{
		dir:   dir,
		runs:  runrepo.New(dir),
		spool: spoolrepo.New(dir),
		disks: diskrepo.NewMockDisk(gomock.NewController(t)),
		free:  100 << 30,
	}

	h.disks.EXPECT().
		Free(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (int64, error) { return h.free, h.freeErr }).
		AnyTimes()

	h.service = executionsvc.New(
		h.runs,
		h.spool,
		h.disks,
		dir,
		config.Runner{Capacity: capacity},
		config.App{Version: "1.4.0"},
		config.Scheduler{MinFreeDisk: watermark},
	)

	return h
}

func newHarnessOver(t *testing.T, first *harness, capacity int, watermark int64) *harness {
	t.Helper()

	h := &harness{
		dir:   first.dir,
		runs:  runrepo.New(first.dir),
		spool: spoolrepo.New(first.dir),
		disks: diskrepo.NewMockDisk(gomock.NewController(t)),
		free:  first.free,
	}

	h.disks.EXPECT().
		Free(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (int64, error) { return h.free, h.freeErr }).
		AnyTimes()

	h.service = executionsvc.New(
		h.runs,
		h.spool,
		h.disks,
		h.dir,
		config.Runner{Capacity: capacity},
		config.App{Version: "1.4.0"},
		config.Scheduler{MinFreeDisk: watermark},
	)

	return h
}

func (h *harness) offer(id string) channelv1.Offer {
	return channelv1.Offer{
		ExecutionID: id,
		Reference:   "NORN-45",
		Attempt:     1,
		WorkspaceID: "01WORKSPACE",
		Issue:       channelv1.Issue{Reference: "NORN-45", Title: "Channel client"},
		Params:      channelv1.Params{Tool: "claude-code"},
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
