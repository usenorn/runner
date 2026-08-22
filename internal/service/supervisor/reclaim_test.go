package supervisor_test

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func stray(t *testing.T) (int, <-chan struct{}) {
	t.Helper()

	command := exec.Command("sh", "-c", "sleep 300")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := command.Start(); err != nil {
		t.Fatalf("start something for an earlier machine to have left behind: %v", err)
	}

	pid := command.Process.Pid
	gone := make(chan struct{})

	go func() {
		defer close(gone)

		_ = command.Wait()
	}()

	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-gone
	})

	return pid, gone
}

func TestAMachineThatRestartedStopsWhatItsLastRunLeftRunning(t *testing.T) {
	h := newHarness(t, 47400, 47499)

	execution := h.prepared(t, "exec-01LEFTOVER")
	left, gone := stray(t)

	if err := h.runs.SaveServices(context.Background(), execution.ID, entity.RunServices{
		Runtime: entity.RuntimeProcess,
		Chosen:  "the test asked for it",
		Ports:   map[string]int{"api": 47401},
		Services: []entity.ServiceRecord{{
			Name:      "api",
			Command:   []string{"sh", "-c", "sleep 300"},
			Port:      47401,
			PID:       left,
			State:     entity.ServiceHealthy,
			StartedAt: time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatalf("write down what an earlier machine was running: %v", err)
	}

	stop := h.start(t)

	defer stop()

	select {
	case <-gone:
	case <-time.After(patience):
		t.Fatalf("what the earlier machine left behind is still running")
	}

	h.await(t, "waited for the run to say its service had stopped", func() bool {
		record, found := h.stored(t, execution.ID).Service("api")

		return found && record.State == entity.ServiceStopped && record.PID == 0
	})

	said := h.awaitSaid(t, execution.ID, "api is stopped")

	if !strings.Contains(said, "restarted") {
		t.Fatalf("the timeline does not say why the service stopped: %q", said)
	}
}

func TestAProcessIdAnEarlierRunRecordedIsNotKilledWhenItIsSomethingElseNow(t *testing.T) {
	h := newHarness(t, 47500, 47599)

	execution := h.prepared(t, "exec-01REUSED")
	other, gone := stray(t)

	if err := h.runs.SaveServices(context.Background(), execution.ID, entity.RunServices{
		Runtime: entity.RuntimeProcess,
		Services: []entity.ServiceRecord{{
			Name:    "api",
			Command: []string{"go", "run", "./cmd/api"},
			PID:     other,
			State:   entity.ServiceStarting,
		}},
	}); err != nil {
		t.Fatalf("write down what an earlier machine was running: %v", err)
	}

	stop := h.start(t)

	defer stop()

	h.await(t, "waited for the run to say its service had stopped", func() bool {
		record, found := h.stored(t, execution.ID).Service("api")

		return found && record.State == entity.ServiceStopped
	})

	select {
	case <-gone:
		t.Fatalf(
			"a process this machine never started was killed because an earlier run happened " +
				"to record its process id",
		)
	default:
	}
}
