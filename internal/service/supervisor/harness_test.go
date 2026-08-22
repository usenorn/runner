package supervisor_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	portrepo "github.com/usenorn/runner/internal/repository/port"
	processrepo "github.com/usenorn/runner/internal/repository/process"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	servicelogrepo "github.com/usenorn/runner/internal/repository/servicelog"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	"github.com/usenorn/runner/internal/service"
	supervisorsvc "github.com/usenorn/runner/internal/service/supervisor"
)

const patience = 15 * time.Second

type harness struct {
	dir     *statedir.Dir
	runs    repository.Run
	spool   repository.Spool
	ports   repository.Port
	service service.Services
}

func newHarness(t *testing.T, lowest int, highest int) *harness {
	t.Helper()

	shell(t)

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	return over(t, dir, lowest, highest)
}

func over(t *testing.T, dir *statedir.Dir, lowest int, highest int) *harness {
	t.Helper()

	h := &harness{
		dir:   dir,
		runs:  runrepo.New(dir),
		spool: spoolrepo.New(dir),
		ports: portrepo.New(config.Runner{PortRange: [2]int{lowest, highest}}),
	}

	h.service = supervisorsvc.New(
		processrepo.New(),
		h.ports,
		servicelogrepo.New(dir),
		h.runs,
		h.spool,
		settings(),
	)

	return h
}

func settings() config.Supervisor {
	return config.Supervisor{
		HealthInterval:  20 * time.Millisecond,
		HealthTimeout:   3 * time.Second,
		StopGrace:       2 * time.Second,
		RestartAttempts: 3,
		RestartBackoff:  10 * time.Millisecond,
		StepTimeout:     10 * time.Second,
	}
}

func shell(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("there is no shell here, so no service can be started")
	}
}

func (h *harness) prepared(t *testing.T, executionID string) entity.Execution {
	t.Helper()

	if _, err := h.runs.Open(context.Background(), executionID); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	execution := entity.Execution{
		ID:         executionID,
		Reference:  "NORN-48",
		IssueKey:   "NORN-48",
		Attempt:    1,
		Title:      "Service supervisor",
		Directory:  h.dir.Run(executionID),
		State:      channelv1.StatePreparing,
		AcceptedAt: time.Now().UTC(),
	}

	if err := h.runs.SaveTask(context.Background(), execution); err != nil {
		t.Fatalf("write down a run: %v", err)
	}

	return execution
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

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("%s, and it never did", what)
}

func (h *harness) awaitState(
	t *testing.T,
	executionID string,
	name string,
	state entity.ServiceState,
) entity.ServiceRecord {
	t.Helper()

	var found entity.ServiceRecord

	h.await(t, "waited for "+name+" to be "+string(state), func() bool {
		for _, record := range h.list(t, executionID) {
			if record.Name == name && record.State == state {
				found = record

				return true
			}
		}

		return false
	})

	return found
}

func (h *harness) list(t *testing.T, executionID string) []entity.ServiceRecord {
	t.Helper()

	records, err := h.service.List(context.Background(), executionID)
	if err != nil {
		t.Fatalf("list the services of %s: %v", executionID, err)
	}

	return records
}

func (h *harness) awaitSaid(t *testing.T, executionID string, wanted string) string {
	t.Helper()

	var found string

	h.await(t, "waited for the timeline of "+executionID+" to say "+wanted, func() bool {
		for _, entry := range h.timeline(t, executionID) {
			if strings.Contains(entry.Reason, wanted) {
				found = entry.Reason

				return true
			}
		}

		return false
	})

	return found
}

func (h *harness) timeline(t *testing.T, executionID string) []entity.TimelineEntry {
	t.Helper()

	entries, err := h.runs.Timeline(context.Background(), executionID)
	if err != nil {
		return nil
	}

	return entries
}

func (h *harness) awaitReported(t *testing.T, executionID string, wanted string) {
	t.Helper()

	h.await(t, "waited for norn to be told "+wanted, func() bool {
		for _, said := range h.reported(t, executionID) {
			if strings.Contains(said, wanted) {
				return true
			}
		}

		return false
	})
}

func (h *harness) reported(t *testing.T, executionID string) []string {
	t.Helper()

	waiting, err := h.spool.Head(context.Background(), 0)
	if err != nil {
		t.Fatalf("read the spool: %v", err)
	}

	said := []string{}

	for _, message := range waiting {
		if message.Type != channelv1.ExecutionEvent || message.ExecutionID != executionID {
			continue
		}

		var entry channelv1.Entry

		if err := json.Unmarshal(message.Payload, &entry); err != nil {
			t.Fatalf("read an execution event: %v", err)
		}

		if entry.Kind == string(channelv1.EventService) {
			said = append(said, entry.Reason)
		}
	}

	return said
}

func (h *harness) stored(t *testing.T, executionID string) entity.RunServices {
	t.Helper()

	services, err := h.runs.LoadServices(context.Background(), executionID)
	if err != nil {
		t.Fatalf("read what a run wrote down about its services: %v", err)
	}

	return services
}

func (h *harness) wrote(t *testing.T, executionID string, name string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(
		h.dir.Run(executionID), entity.RunLogsDir, entity.RunServiceLogsDir, name+".log",
	))
	if err != nil {
		return ""
	}

	return string(raw)
}

func held(name string, command string) entity.Service {
	return entity.Service{Name: name, Command: []string{"sh", "-c", command}}
}
