package control_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
)

func TestAQuestionNobodyAnswersComesBackTellingTheAgentToStop(t *testing.T) {
	h := newHarness(t, nil)

	running(t, h, "exec-01ASK")

	answered, err := h.as(t, "exec-01ASK").Ask(context.Background(), "exec-01ASK", control.QuestionRequest{
		Blocking: true,
		Message:  "Keep the old endpoint?",
		Options:  []string{"Keep for 30 days", "Remove now"},
	})
	if err != nil {
		t.Fatalf("ask a question over the socket: %v", err)
	}

	if answered.Status != string(entity.AskPending) {
		t.Fatalf("the agent was told %q, want it left pending", answered.Status)
	}

	if answered.QuestionID == "" {
		t.Fatal("the question came back with no id, so nothing can be matched to its answer")
	}

	if !strings.Contains(answered.Advice, "end your turn") {
		t.Fatalf("the agent was left with %q, which does not tell it to stop", answered.Advice)
	}
}

func TestAskingHoldsTheSocketOpenForAsLongAsTheDaemonHoldsTheQuestion(t *testing.T) {
	h := newHarness(t, nil)

	running(t, h, "exec-01PATIENT")

	// The daemon holds this question far longer than an ordinary control request is given, which
	// is the whole point: the client has to be patient about this one call and no other.
	impatient := settings()
	impatient.RequestTimeout = 50 * time.Millisecond

	client := control.NewClient(impatient, questionSettings(), stepSettings(), h.dir, h.bearer(t, "exec-01PATIENT"))

	began := time.Now()

	answered, err := client.Ask(context.Background(), "exec-01PATIENT", control.QuestionRequest{
		Blocking: true,
		Message:  "Keep the old endpoint?",
	})
	if err != nil {
		t.Fatalf(
			"asking gave up before the daemon had finished holding the question: %v. An agent "+
				"told its own tool timed out reasonably asks again, and every retry puts another "+
				"copy of the question in front of a person",
			err,
		)
	}

	if answered.Status != string(entity.AskPending) {
		t.Fatalf("the agent was told %q, want it left pending", answered.Status)
	}

	if waited := time.Since(began); waited < questionSettings().SoftWait {
		t.Fatalf("asking came back after %s without waiting for an answer at all", waited)
	}
}

func TestAQuestionAgainstARunThisMachineIsNotHoldingIsRefused(t *testing.T) {
	h := newHarness(t, nil)

	_, err := h.as(t, "exec-01NOSUCH").Ask(context.Background(), "exec-01NOSUCH", control.QuestionRequest{
		Blocking: true,
		Message:  "Keep the old endpoint?",
	})

	if err == nil {
		t.Fatal(
			"a question was taken against a run this machine is not holding, so an agent could " +
				"put questions on somebody else's issue",
		)
	}
}

func TestAnOptionTooLongForNornIsRefusedToTheAgentWithTheLimit(t *testing.T) {
	h := newHarness(t, nil)

	running(t, h, "exec-01LONG")

	_, err := h.as(t, "exec-01LONG").Ask(context.Background(), "exec-01LONG", control.QuestionRequest{
		Blocking: true,
		Message:  "How should the socket authenticate?",
		Options:  []string{"A short-lived token", strings.Repeat("a ticket from the platform ", 9)},
	})

	if err == nil || !strings.Contains(err.Error(), "option 2") || !strings.Contains(err.Error(), "at most 200") {
		t.Fatalf(
			"an option norn would refuse came back to the agent as %v; it has to say which option "+
				"and how long it may be, or the question is lost on the way to norn",
			err,
		)
	}
}

func TestTheArtifactsAQuestionPointsAtReachNorn(t *testing.T) {
	h := newHarness(t, nil)

	running(t, h, "exec-01ART")

	if _, err := h.as(t, "exec-01ART").Ask(context.Background(), "exec-01ART", control.QuestionRequest{
		Blocking:  true,
		Message:   "Does this chart read right?",
		Artifacts: []string{"f8b0a1c2-0000-4000-8000-000000000001"},
	}); err != nil {
		t.Fatalf("ask: %v", err)
	}

	held, err := spoolrepo.New(h.dir).Head(context.Background(), 16)
	if err != nil {
		t.Fatalf("read the spool: %v", err)
	}

	for _, message := range held {
		if message.Type != channelv1.QuestionAsked {
			continue
		}

		var asked channelv1.Question
		if err := json.Unmarshal(message.Payload, &asked); err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(asked.Context.Artifacts, []string{"f8b0a1c2-0000-4000-8000-000000000001"}) {
			t.Fatalf("the question went to norn pointing at %v", asked.Context.Artifacts)
		}

		return
	}

	t.Fatal("the question never reached the spool")
}
