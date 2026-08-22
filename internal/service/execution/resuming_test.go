package execution_test

import (
	"context"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func stopped() entity.Question {
	return entity.Question{
		Kind:          entity.QuestionDecision,
		Blocking:      true,
		Message:       "Keep the old endpoint?",
		Options:       []string{"Keep for 30 days", "Remove now"},
		AllowFreeText: true,
	}
}

// asking runs the coding agent's part: it stops to ask while its session is open, nobody answers
// inside the soft wait, and it ends its turn the way the tool tells it to.
func asking(t *testing.T, h *harness, id string) script {
	t.Helper()

	held := holds("session-01")

	go func() {
		<-h.drivers.playing()

		if _, err := h.questions.Ask(context.Background(), id, stopped()); err != nil {
			t.Error(err)
		}

		close(held.hold)
	}()

	return held
}

func TestARunWhoseAgentStoppedToAskParksInsteadOfFinalizing(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{asking(t, h, "exec-01ABC")}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to park on its question", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(channelv1.StateWaitingForInput) {
				return strings.Contains(reported.Reason, "Keep the old endpoint?")
			}
		}

		return false
	})

	for _, reported := range h.reports(t) {
		if reported.State == string(channelv1.StateFinalizing) {
			t.Fatal(
				"a run with a question still in front of a person went on to finalize, so there " +
					"is nothing left to answer into",
			)
		}
	}
}

func TestAnAnsweredRunCarriesOnInTheSameSessionWithTheAnswerInIt(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{
		asking(t, h, "exec-01ABC"),
		finishes("session-01", "the work is committed"),
	}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to park on its question", func() bool {
		return held(h.reports(t), channelv1.StateWaitingForInput)
	})

	if err := h.questions.Answered(context.Background(), "exec-01ABC", entity.Answer{
		QuestionID: "q-1", Answer: "Remove now", AnsweredBy: "Rae", AnsweredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("hand the run its answer: %v", err)
	}

	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason: channelv1.ResumeAnswer, Instruction: "Remove now", QuestionID: "q-1",
	}); err != nil {
		t.Fatalf("ask the run to carry on: %v", err)
	}

	h.await(t, "waited for the answered run to finish", func() bool {
		return held(h.reports(t), channelv1.StateFinalizing)
	})

	carried := h.drivers.carried()
	if len(carried) != 1 {
		t.Fatalf("the agent was resumed %d times, want once", len(carried))
	}

	if carried[0].ID != "session-01" {
		t.Fatalf(
			"the run carried on in session %q rather than session-01, so the agent lost "+
				"everything it had already worked out",
			carried[0].ID,
		)
	}

	said := h.drivers.injections()
	if len(said) != 1 || !strings.Contains(said[0], "Remove now") {
		t.Fatalf("the agent was told %q, want the answer word for word", said)
	}

	if !strings.Contains(said[0], "Keep the old endpoint?") {
		t.Fatalf(
			"the agent was handed an answer with no question attached: %q. It has no way to tell "+
				"what it was answering",
			said[0],
		)
	}
}

func TestAResumeSettlesTheQuestionEvenWhenTheAnswerCameOnlyWithIt(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{
		asking(t, h, "exec-01ABC"),
		finishes("session-01", "the work is committed"),
	}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to park on its question", func() bool {
		return held(h.reports(t), channelv1.StateWaitingForInput)
	})

	// Norn moved the run on the strength of the answer alone, which is what it does when somebody
	// answers a run that has already parked. Nothing separate ever tells this machine.
	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason: channelv1.ResumeAnswer, Instruction: "Remove now", QuestionID: "q-1",
	}); err != nil {
		t.Fatalf("ask the run to carry on: %v", err)
	}

	h.await(t, "waited for the answered run to finish", func() bool {
		return held(h.reports(t), channelv1.StateFinalizing)
	})

	if _, waiting := h.questions.Waiting("exec-01ABC"); waiting {
		t.Fatal(
			"the run is still holding the question it was answered on, so the next thing it " +
				"finishes would park it again on a question nobody is waiting to answer",
		)
	}
}

func TestARunNobodyAnsweredIsNotCarriedOnByAResumeForSomethingElse(t *testing.T) {
	h := newHarness(t, 2, 0)

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to finish", func() bool {
		return held(h.reports(t), channelv1.StateFinalizing)
	})

	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason: channelv1.ResumeAnswer, Instruction: "Remove now",
	}); err != nil {
		t.Fatalf("ask a run that is not waiting to carry on: %v", err)
	}

	if len(h.drivers.carried()) != 0 {
		t.Fatal(
			"a run that was not waiting on anybody was started again, so a finished session is " +
				"reopened against work that is already collected",
		)
	}
}

func held(reports []channelv1.Report, state channelv1.State) bool {
	for _, reported := range reports {
		if reported.State == string(state) {
			return true
		}
	}

	return false
}
