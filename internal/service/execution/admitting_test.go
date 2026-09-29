package execution_test

import (
	"context"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func stateOf(h *harness, executionID string) entity.ExecutionState {
	for _, execution := range h.service.Report(context.Background()).Executions {
		if execution.ID == executionID {
			return execution.State
		}
	}

	return ""
}

func TestAnAnsweredRunWaitsForASlotRatherThanRunningPastTheMachinesCapacity(t *testing.T) {
	h := newHarness(t, 1, 0)
	busy := holds("session-02")
	h.drivers.scripts = []script{
		asking(t, h, "exec-01ABC"),
		busy,
		finishes("session-01", "the work is committed"),
	}

	stop := h.start(t)
	defer stop()

	ctx := context.Background()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the first run to park on its question", func() bool {
		return stateOf(h, "exec-01ABC") == channelv1.StateWaitingForInput
	})

	begun(t, h, "exec-01DEF")

	h.await(t, "waited for the second run to take the free slot", func() bool {
		return stateOf(h, "exec-01DEF") == channelv1.StateRunning
	})

	if err := h.service.Continue(ctx, "exec-01ABC", channelv1.Instruction{
		Reason: channelv1.ResumeAnswer, Answers: []channelv1.Answer{{
			QuestionID: "q-1", Question: "Keep the old endpoint?", Answer: "Remove now", AnsweredBy: "Rae",
		}},
	}); err != nil {
		t.Fatalf("ask the first run to carry on: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if carried := h.drivers.carried(); len(carried) != 0 {
		t.Fatalf(
			"the answered run started again while the machine's one slot was taken: %+v. Every "+
				"answer arriving at once would otherwise run as many agents as there are "+
				"questions, whatever the machine was set to hold",
			carried,
		)
	}

	if state := stateOf(h, "exec-01ABC"); state != channelv1.StateQueuedForResume {
		t.Fatalf("the answered run reads as %s while it waits, want %s", state, channelv1.StateQueuedForResume)
	}

	close(busy.hold)

	h.await(t, "waited for the answered run to carry on once the slot freed up", func() bool {
		return len(h.drivers.carried()) == 1
	})
}

func TestAStartArrivingAfterNornLoweredTheCapacityWaitsForTheRunAheadOfIt(t *testing.T) {
	h := newHarness(t, 2, 0)
	busy := holds("session-01")
	h.drivers.scripts = []script{busy, finishes("session-02", "the work is committed")}

	stop := h.start(t)
	defer stop()

	ctx := context.Background()

	begun(t, h, "exec-01ABC")

	if err := h.service.Offer(ctx, h.offer("exec-01DEF")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	h.await(t, "waited for the first run to be under way", func() bool {
		return stateOf(h, "exec-01ABC") == channelv1.StateRunning
	})

	lowered := 1
	h.service.Configure(channelv1.Configuration{Capacity: &lowered})

	if err := h.service.Start(ctx, "exec-01DEF", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if state := stateOf(h, "exec-01DEF"); state != channelv1.StateLeased {
		t.Fatalf("the second run went to %s while the one slot left was taken", state)
	}

	close(busy.hold)

	h.await(t, "waited for the second run to start once the slot freed up", func() bool {
		return len(h.drivers.began()) == 2
	})
}

func TestACancelledRunWaitingForASlotIsNeverStarted(t *testing.T) {
	h := newHarness(t, 2, 0)
	busy := holds("session-01")
	h.drivers.scripts = []script{busy, finishes("session-02", "the work is committed")}

	stop := h.start(t)
	defer stop()

	ctx := context.Background()

	begun(t, h, "exec-01ABC")

	if err := h.service.Offer(ctx, h.offer("exec-01DEF")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	lowered := 1
	h.service.Configure(channelv1.Configuration{Capacity: &lowered})

	if err := h.service.Start(ctx, "exec-01DEF", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := h.service.Cancel(ctx, "exec-01DEF", "the person changed their mind"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	close(busy.hold)

	h.await(t, "waited for the first run to finish", func() bool {
		return stateOf(h, "exec-01ABC") == channelv1.StateAwaitingReview
	})

	time.Sleep(50 * time.Millisecond)

	if began := h.drivers.began(); len(began) != 1 {
		t.Fatalf("a run cancelled while it waited for a slot was started anyway: %d agents began", len(began))
	}
}
