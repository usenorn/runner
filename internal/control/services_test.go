package control_test

import (
	"context"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
	runrepo "github.com/usenorn/runner/internal/repository/run"
)

func running(t *testing.T, h *harness, executionID string) {
	t.Helper()

	runs := runrepo.New(h.dir)
	ctx := context.Background()

	if _, err := runs.Open(ctx, executionID); err != nil {
		t.Fatalf("make a run directory by hand: %v", err)
	}

	if err := runs.SaveTask(ctx, entity.Execution{
		ID:         executionID,
		Reference:  "NORN-48",
		IssueKey:   "NORN-48",
		Attempt:    1,
		Title:      "Service supervisor",
		Directory:  h.dir.Run(executionID),
		State:      channelv1.StatePreparing,
		AcceptedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write a task by hand: %v", err)
	}
}

func TestAServiceIsStartedListedAndStoppedOverTheSocket(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	running(t, h, "exec-01SOCKET")

	started, err := h.as(t, "exec-01SOCKET").StartService(ctx, "exec-01SOCKET", control.ServiceRequest{
		Name:    "api",
		Command: []string{"sh", "-c", "echo listening on $NORN_PORT_API; sleep 300"},
		Health:  control.Health{Kind: string(entity.HealthLog), Pattern: "listening on"},
	})
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	if started.Port == 0 || started.PID == 0 {
		t.Fatalf("the service came back as %+v", started)
	}

	services, err := h.as(t, "exec-01SOCKET").Services(ctx, "exec-01SOCKET")
	if err != nil {
		t.Fatalf("list the services: %v", err)
	}

	if len(services) != 1 || services[0].Name != "api" {
		t.Fatalf("the run listed %+v", services)
	}

	waitFor(t, func() bool {
		held, err := h.as(t, "exec-01SOCKET").ServiceLogs(ctx, "exec-01SOCKET", "api", entity.LogQuery{})

		return err == nil && strings.Contains(strings.Join(held.Lines, "\n"), "listening on")
	})

	stopped, err := h.as(t, "exec-01SOCKET").StopService(ctx, "exec-01SOCKET", "api")
	if err != nil {
		t.Fatalf("stop the service: %v", err)
	}

	if stopped.State != string(entity.ServiceStopped) {
		t.Fatalf("a service that was asked to stop came back %s", stopped.State)
	}
}

func TestAStepRunsOverTheSocketAndHandsBackWhatItWrote(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	running(t, h, "exec-01STEP")

	result, err := h.as(t, "exec-01STEP").RunStep(ctx, "exec-01STEP", control.StepRequest{
		Name:    "deps",
		Command: []string{"sh", "-c", "echo pulled 12 packages"},
	})
	if err != nil {
		t.Fatalf("run a step: %v", err)
	}

	if result.ExitCode != 0 || !strings.Contains(result.Output, "pulled 12 packages") {
		t.Fatalf("the step came back as %+v", result)
	}
}

func TestAskingAboutAServiceThisRunNeverStartedSaysSo(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	running(t, h, "exec-01NOSERVICE")

	if _, err := h.as(t, "exec-01NOSERVICE").StopService(ctx, "exec-01NOSERVICE", "ghost"); err == nil ||
		!strings.Contains(err.Error(), "no service by that name") {
		t.Fatalf("stopping a service nothing started answered %v", err)
	}

	if _, err := h.as(t, "exec-01GONE").Services(ctx, "exec-01GONE"); err == nil ||
		!strings.Contains(err.Error(), "not holding that execution") {
		t.Fatalf("listing the services of a run this machine never had answered %v", err)
	}
}

func waitFor(t *testing.T, until func() bool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		if until() {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("waited for the runner to answer, and it never did")
}
