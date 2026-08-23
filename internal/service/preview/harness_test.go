package preview_test

import (
	"context"
	"os"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	"github.com/usenorn/runner/internal/service"
	previewsvc "github.com/usenorn/runner/internal/service/preview"
)

type harness struct {
	runs    repository.Run
	spool   repository.Spool
	service service.Previews
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	root, err := os.MkdirTemp("/tmp", "nrn")
	if err != nil {
		t.Fatalf("create temporary root: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	runs := runrepo.New(dir)
	spool := spoolrepo.New(dir)

	return &harness{runs: runs, spool: spool, service: previewsvc.New(runs, spool)}
}

func (h *harness) running(t *testing.T, executionID string, records ...entity.ServiceRecord) {
	t.Helper()

	ctx := context.Background()

	if _, err := h.runs.Open(ctx, executionID); err != nil {
		t.Fatalf("make a run directory: %v", err)
	}

	if err := h.runs.SaveTask(ctx, entity.Execution{
		ID:         executionID,
		Reference:  "NORN-52",
		IssueKey:   "NORN-52",
		Attempt:    1,
		Title:      "Norn's own tools",
		State:      channelv1.StateRunning,
		AcceptedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write a task: %v", err)
	}

	ports := map[string]int{}

	for _, record := range records {
		ports[record.Name] = record.Port
	}

	if err := h.runs.SaveServices(ctx, executionID, entity.RunServices{
		Runtime:  "process",
		Ports:    ports,
		Services: records,
	}); err != nil {
		t.Fatalf("write the services: %v", err)
	}
}

func serving(name string, port int, state entity.ServiceState) entity.ServiceRecord {
	return entity.ServiceRecord{
		Name:      name,
		Command:   []string{"pnpm", "dev"},
		Port:      port,
		PID:       4242,
		State:     state,
		StartedAt: time.Now().UTC(),
	}
}
