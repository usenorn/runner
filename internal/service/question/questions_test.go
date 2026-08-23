package question_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	"github.com/usenorn/runner/internal/service"
	questionsvc "github.com/usenorn/runner/internal/service/question"
)

const run = "exec-01ABC"

type spoolStub struct {
	sent chan channelv1.Message
}

func newSpool() *spoolStub {
	return &spoolStub{sent: make(chan channelv1.Message, 8)}
}

func (s *spoolStub) Append(_ context.Context, message channelv1.Message) error {
	s.sent <- message

	return nil
}

func (s *spoolStub) Head(context.Context, int) ([]channelv1.Message, error) { return nil, nil }
func (s *spoolStub) Acknowledge(context.Context, string) error              { return nil }
func (s *spoolStub) Count(context.Context) (int, error)                     { return 0, nil }

func (s *spoolStub) Prune(context.Context, time.Time, int) (int, error) { return 0, nil }

func newQuestions(t *testing.T, wait time.Duration) (service.Questions, *spoolStub) {
	t.Helper()

	dir, err := statedir.New(config.State{Root: filepath.Join(t.TempDir(), "norn")})
	if err != nil {
		t.Fatalf("open a state directory: %v", err)
	}

	runs := runrepo.New(dir)
	ctx := context.Background()

	if _, err := runs.Open(ctx, run); err != nil {
		t.Fatalf("make a run directory: %v", err)
	}

	if err := runs.SaveTask(ctx, entity.Execution{
		ID:         run,
		Reference:  "NORN-37",
		IssueKey:   "NORN-37",
		Attempt:    1,
		Directory:  dir.Run(run),
		State:      channelv1.StateRunning,
		AcceptedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write a task: %v", err)
	}

	spool := newSpool()

	return questionsvc.New(
		runs, spool, config.Questions{SoftWait: wait, MaxWait: time.Minute},
	), spool
}

func blocking() entity.Question {
	return entity.Question{
		Kind:          entity.QuestionDecision,
		Blocking:      true,
		Message:       "Keep the old endpoint?",
		Options:       []string{"Keep for 30 days", "Remove now"},
		AllowFreeText: true,
	}
}

func sentQuestion(t *testing.T, spool *spoolStub) channelv1.Question {
	t.Helper()

	select {
	case message := <-spool.sent:
		if message.Type != channelv1.QuestionAsked {
			t.Fatalf("a %s went to norn, want a question", message.Type)
		}

		var asked channelv1.Question
		if err := json.Unmarshal(message.Payload, &asked); err != nil {
			t.Fatalf("read the question that went to norn: %v", err)
		}

		return asked
	case <-time.After(time.Second):
		t.Fatal("nothing went to norn, so nobody will ever see the question")

		return channelv1.Question{}
	}
}

func TestAnAnswerThatArrivesWhileTheAgentIsWaitingComesBackInline(t *testing.T) {
	questions, spool := newQuestions(t, time.Second)
	ctx := context.Background()

	answered := make(chan entity.Asked, 1)

	go func() {
		asked, err := questions.Ask(ctx, run, blocking())
		if err != nil {
			t.Error(err)
		}

		answered <- asked
	}()

	asked := sentQuestion(t, spool)

	deadline := time.After(time.Second)

	for {
		err := questions.Answered(ctx, run, entity.Answer{
			QuestionID: "q-1", Ref: asked.Ref, Answer: "Remove now", AnsweredBy: "Rae",
		})
		if err != nil {
			t.Fatalf("hand the run its answer: %v", err)
		}

		select {
		case got := <-answered:
			if got.Outcome != entity.AskAnswered {
				t.Fatalf("the agent was told %q, want it answered without stopping", got.Outcome)
			}

			if got.Answer != "Remove now" || got.AnsweredBy != "Rae" {
				t.Fatalf("the agent got %+v, want the answer and who gave it", got)
			}

			return
		case <-deadline:
			t.Fatal("an answer that arrived in time never reached the agent")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestAnAgentNobodyAnswersInTimeIsToldToStopRatherThanGuess(t *testing.T) {
	questions, spool := newQuestions(t, 20*time.Millisecond)
	ctx := context.Background()

	asked, err := questions.Ask(ctx, run, blocking())
	if err != nil {
		t.Fatalf("ask a question: %v", err)
	}

	sentQuestion(t, spool)

	if asked.Outcome != entity.AskPending {
		t.Fatalf("the agent was told %q after nobody answered, want it left pending", asked.Outcome)
	}

	if asked.Advice == "" {
		t.Fatal("the agent was left pending with no instruction, so it will guess and carry on")
	}

	if _, waiting := questions.Waiting(run); !waiting {
		t.Fatal(
			"the run does not read as waiting once its soft wait lapsed, so it would finalize " +
				"with a question still in front of a person",
		)
	}
}

func TestAnAnswerThatArrivesAfterTheWaitIsKeptForTheWayBackIn(t *testing.T) {
	questions, spool := newQuestions(t, 20*time.Millisecond)
	ctx := context.Background()

	if _, err := questions.Ask(ctx, run, blocking()); err != nil {
		t.Fatalf("ask a question: %v", err)
	}

	asked := sentQuestion(t, spool)

	if err := questions.Answered(ctx, run, entity.Answer{
		QuestionID: "q-1", Ref: asked.Ref, Answer: "Remove now", AnsweredBy: "Rae",
	}); err != nil {
		t.Fatalf("hand the run its answer: %v", err)
	}

	if _, waiting := questions.Waiting(run); waiting {
		t.Fatal("a run that has its answer still reads as waiting, so it would park with nothing to wait for")
	}

	question, answer, held := questions.Take(run)
	if !held {
		t.Fatal(
			"the answer that arrived after the wait was dropped, so the run resumes with nothing " +
				"to tell the agent",
		)
	}

	if answer.Answer != "Remove now" || question.Message != blocking().Message {
		t.Fatalf("the run kept %+v against %q, want both as asked and answered", answer, question.Message)
	}

	if _, _, again := questions.Take(run); again {
		t.Fatal("the same answer was handed out twice, so a second resume would replay it")
	}
}

func TestAQuestionTheAgentIsNotWaitingOnDoesNotStopIt(t *testing.T) {
	questions, spool := newQuestions(t, time.Minute)
	ctx := context.Background()

	meanwhile := blocking()
	meanwhile.Blocking = false
	meanwhile.Default = "keep it for 30 days"

	asked, err := questions.Ask(ctx, run, meanwhile)
	if err != nil {
		t.Fatalf("ask a question the agent is not waiting on: %v", err)
	}

	if asked.Outcome != entity.AskNoted {
		t.Fatalf("the agent was told %q and held for a minute, want it noted and let go", asked.Outcome)
	}

	if sentQuestion(t, spool).Default != meanwhile.Default {
		t.Fatal("what the agent said it would do meanwhile never reached norn")
	}

	if _, waiting := questions.Waiting(run); waiting {
		t.Fatal("a run that carried on reads as waiting, so it would park at the end for nothing")
	}
}

func TestAQuestionNobodyCouldAnswerIsRefusedBeforeItLeavesTheMachine(t *testing.T) {
	questions, spool := newQuestions(t, time.Second)

	unanswerable := blocking()
	unanswerable.Options = nil
	unanswerable.AllowFreeText = false

	if _, err := questions.Ask(context.Background(), run, unanswerable); !errors.Is(
		err, entity.ErrQuestionUnreachable,
	) {
		t.Fatalf("a question with no options and no free text was accepted, answering %v", err)
	}

	select {
	case <-spool.sent:
		t.Fatal("a question nobody can answer was still sent to norn")
	default:
	}
}

func TestAQuestionTheAgentWillNotWaitOnHasToSayWhatItDoesMeanwhile(t *testing.T) {
	questions, _ := newQuestions(t, time.Second)

	undeclared := blocking()
	undeclared.Blocking = false

	if _, err := questions.Ask(context.Background(), run, undeclared); !errors.Is(
		err, entity.ErrQuestionUndeclared,
	) {
		t.Fatalf(
			"an agent carried on past its own question without saying what it was carrying on "+
				"with, and it was accepted, answering %v",
			err,
		)
	}
}

func TestAQuestionAgainstARunThisMachineIsNotWorkingOnIsRefused(t *testing.T) {
	questions, spool := newQuestions(t, time.Second)

	if _, err := questions.Ask(context.Background(), "exec-01OTHER", blocking()); !errors.Is(
		err, entity.ErrQuestionUnknownRun,
	) {
		t.Fatalf(
			"a question was taken against a run this machine is not working on, answering %v; an "+
				"agent could put questions on somebody else's issue",
			err,
		)
	}

	select {
	case <-spool.sent:
		t.Fatal("the question still went to norn")
	default:
	}
}

func TestAnAnswerToAQuestionTheRunHasMovedPastIsNotTakenForTheOneItStoppedOn(t *testing.T) {
	questions, spool := newQuestions(t, 20*time.Millisecond)
	ctx := context.Background()

	if _, err := questions.Ask(ctx, run, blocking()); err != nil {
		t.Fatalf("ask a question: %v", err)
	}

	stale := sentQuestion(t, spool)

	if _, err := questions.Ask(ctx, run, blocking()); err != nil {
		t.Fatalf("ask it again: %v", err)
	}

	sentQuestion(t, spool)

	if err := questions.Answered(ctx, run, entity.Answer{
		QuestionID: "q-1", Ref: stale.Ref, Answer: "Return 0", AnsweredBy: "Rae",
	}); err != nil {
		t.Fatalf("hand the run an answer to an older question: %v", err)
	}

	if _, waiting := questions.Waiting(run); !waiting {
		t.Fatal(
			"an answer to a question the run had already moved past settled the one it is " +
				"actually stopped on",
		)
	}

	if _, _, held := questions.Take(run); held {
		t.Fatal("the run would resume on an answer to a question it did not ask last")
	}
}
