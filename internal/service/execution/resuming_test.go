package execution_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func stopped() entity.Question {
	return entity.Question{
		Kind:     entity.QuestionDecision,
		Blocking: true,
		Message:  "Keep the old endpoint?",
		Options:  []string{"Keep for 30 days", "Remove now"},
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
		Reason: channelv1.ResumeAnswer, Answers: []channelv1.Answer{{
			QuestionID: "q-1", Question: "Keep the old endpoint?", Answer: "Remove now", AnsweredBy: "Rae",
		}},
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
		Reason: channelv1.ResumeAnswer, Answers: []channelv1.Answer{{
			QuestionID: "q-1", Question: "Keep the old endpoint?", Answer: "Remove now", AnsweredBy: "Rae",
		}},
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

func TestARunNornAskedToCarryOnWritesThatDownBeforeItGoesAnyFurther(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{asking(t, h, "exec-01ABC")}

	stop := h.start(t)

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to park on its question", func() bool {
		return held(h.reports(t), channelv1.StateWaitingForInput)
	})

	stop()

	instruction := channelv1.Instruction{
		Reason: channelv1.ResumeAnswer, Answers: []channelv1.Answer{{
			QuestionID: "q-1", Question: "Keep the old endpoint?", Answer: "Remove now", AnsweredBy: "Rae",
		}},
	}

	if err := h.service.Continue(context.Background(), "exec-01ABC", instruction); err != nil {
		t.Fatalf("ask the run to carry on: %v", err)
	}

	kept, err := h.runs.LoadResume(context.Background(), "exec-01ABC")
	if err != nil || !reflect.DeepEqual(kept, instruction) {
		t.Fatalf(
			"the machine kept %+v (%v) of being asked to carry on. Norn counts the message as "+
				"delivered once this returns, so a machine that stops before the agent starts "+
				"again would never hear it a second time",
			kept, err,
		)
	}

	task, err := h.runs.LoadTask(context.Background(), "exec-01ABC")
	if err != nil || task.State != channelv1.StateQueuedForResume {
		t.Fatalf("the run is written down as %s (%v), want %s", task.State, err, channelv1.StateQueuedForResume)
	}
}

func TestARunWaitingOnAnAnswerSurvivesTheMachineRestartingAndCarriesOnInItsSession(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{asking(t, h, "exec-01ABC")}

	stop := h.start(t)

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to park on its question", func() bool {
		return held(h.reports(t), channelv1.StateWaitingForInput)
	})

	stop()

	restarted := newHarnessOver(t, h, 2, 0)
	restarted.drivers.scripts = []script{finishes("session-01", "the work is committed")}

	settled := restarted.start(t)
	defer settled()

	ctx := context.Background()

	if kept := restarted.service.Report(ctx).Executions; len(kept) != 1 ||
		kept[0].State != channelv1.StateWaitingForInput {
		t.Fatalf(
			"after a restart the machine holds %+v. A run waiting on a person has no process to "+
				"lose, so a restart that throws it away costs somebody an answer they already "+
				"gave or are about to",
			kept,
		)
	}

	if question, waiting := restarted.questions.Waiting("exec-01ABC"); !waiting ||
		question.Message != stopped().Message {
		t.Fatalf("after a restart the run is waiting=%v on %+v, want the question it asked", waiting, question)
	}

	if err := restarted.service.Continue(ctx, "exec-01ABC", channelv1.Instruction{
		Reason: channelv1.ResumeAnswer, Answers: []channelv1.Answer{{
			QuestionID: "q-1", Question: "Keep the old endpoint?", Answer: "Remove now", AnsweredBy: "Rae",
		}},
	}); err != nil {
		t.Fatalf("ask the run to carry on: %v", err)
	}

	restarted.await(t, "waited for the answered run to finish", func() bool {
		return held(restarted.reports(t), channelv1.StateFinalizing)
	})

	carried := restarted.drivers.carried()
	if len(carried) != 1 || carried[0].ID != "session-01" {
		t.Fatalf("after a restart the run carried on in %+v, want session-01", carried)
	}

	said := restarted.drivers.injections()
	if len(said) != 1 || !strings.Contains(said[0], "Keep the old endpoint?") ||
		!strings.Contains(said[0], "Remove now") {
		t.Fatalf("after a restart the agent was told %q, want the question and its answer", said)
	}
}

func TestARunTheMachineWasAboutToCarryOnWithIsCarriedOnAfterARestart(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.drivers.scripts = []script{asking(t, h, "exec-01ABC")}

	stop := h.start(t)

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to park on its question", func() bool {
		return held(h.reports(t), channelv1.StateWaitingForInput)
	})

	stop()

	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason: channelv1.ResumeAnswer, Answers: []channelv1.Answer{{
			QuestionID: "q-1", Question: "Keep the old endpoint?", Answer: "Remove now", AnsweredBy: "Rae",
		}},
	}); err != nil {
		t.Fatalf("ask the run to carry on: %v", err)
	}

	restarted := newHarnessOver(t, h, 2, 0)
	restarted.drivers.scripts = []script{finishes("session-01", "the work is committed")}

	settled := restarted.start(t)
	defer settled()

	restarted.await(t, "waited for the run to carry on by itself after the restart", func() bool {
		return held(restarted.reports(t), channelv1.StateFinalizing)
	})

	said := restarted.drivers.injections()
	if len(said) != 1 || !strings.Contains(said[0], "Remove now") {
		t.Fatalf(
			"the agent was told %q. Norn delivered the answer once and counts it as heard, so "+
				"a machine that forgets it leaves the run queued for good",
			said,
		)
	}
}
