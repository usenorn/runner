package supervisor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func TestAStepHandsBackWhatItWroteAndSaysSoOnTheTimeline(t *testing.T) {
	h := newHarness(t, 47000, 47099)
	stop := h.start(t)

	defer stop()

	execution := h.prepared(t, "exec-01STEP")

	result, err := h.service.Step(context.Background(), execution.ID, entity.Step{
		Name:    "deps",
		Command: []string{"sh", "-c", "echo pulled 12 packages"},
	})
	if err != nil {
		t.Fatalf("run a step: %v", err)
	}

	if result.ExitCode != 0 || !strings.Contains(result.Output, "pulled 12 packages") {
		t.Fatalf("the step came back as %+v", result)
	}

	h.awaitSaid(t, execution.ID, "the step deps finished")

	if !strings.Contains(h.wrote(t, execution.ID, "deps"), "pulled 12 packages") {
		t.Fatalf("what the step wrote is not in the run's own logs")
	}
}

func TestAStepThatFailsKeepsItsCodeAndItsOutputRatherThanBeingAnError(t *testing.T) {
	h := newHarness(t, 47100, 47199)
	stop := h.start(t)

	defer stop()

	execution := h.prepared(t, "exec-01FAILED")

	result, err := h.service.Step(context.Background(), execution.ID, entity.Step{
		Name:    "build",
		Command: []string{"sh", "-c", "echo cannot find module; exit 2"},
	})
	if err != nil {
		t.Fatalf("run a step that fails: %v", err)
	}

	if result.ExitCode != 2 {
		t.Fatalf("a step that chose exit code 2 came back with %d", result.ExitCode)
	}

	if !strings.Contains(result.Output, "cannot find module") {
		t.Fatalf("the step's output came back as %q", result.Output)
	}

	h.awaitSaid(t, execution.ID, "the step build stopped with exit code 2")
}

func TestAStepGivenTooLittleTimeIsStoppedAndSaysHowLongItHad(t *testing.T) {
	h := newHarness(t, 47200, 47299)
	stop := h.start(t)

	defer stop()

	execution := h.prepared(t, "exec-01SLOW")

	result, err := h.service.Step(context.Background(), execution.ID, entity.Step{
		Name:    "migrate",
		Command: []string{"sh", "-c", "echo connecting; sleep 300"},
		Timeout: 200 * time.Millisecond,
	})

	if !errors.Is(err, entity.ErrStepTimedOut) {
		t.Fatalf("a step given no time at all answered %v", err)
	}

	if !result.TimedOut {
		t.Fatalf("the step does not say it ran out of time: %+v", result)
	}

	if !strings.Contains(result.Output, "connecting") {
		t.Fatalf("what the step wrote before it was stopped was lost: %q", result.Output)
	}

	said := h.awaitSaid(t, execution.ID, "the step migrate was given")

	if !strings.Contains(said, "200ms") {
		t.Fatalf("the timeline does not say how long the step had: %q", said)
	}
}

func TestAStepReachesAServicesPortByTheNameItWasGiven(t *testing.T) {
	h := newHarness(t, 47300, 47399)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()
	execution := h.prepared(t, "exec-01SEED")

	defer func() { _ = h.service.Release(ctx, execution.ID) }()

	api, err := h.service.Start(ctx, execution.ID, held("api", "sleep 300"))
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	result, err := h.service.Step(ctx, execution.ID, entity.Step{
		Name:    "seed",
		Command: []string{"sh", "-c", "echo talking to ${ports.api}"},
	})
	if err != nil {
		t.Fatalf("run a step: %v", err)
	}

	if !strings.Contains(result.Output, "talking to "+itoa(api.Port)) {
		t.Fatalf("the step was not told where the api is: %q", result.Output)
	}
}
