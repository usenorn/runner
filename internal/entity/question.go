package entity

import (
	"errors"
	"slices"
	"strings"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

const (
	QuestionOptionsMax = 8
	QuestionTextMax    = 1000
)

var (
	ErrQuestionUnknownRun  = errors.New("this machine is not running that execution")
	ErrQuestionUnreachable = errors.New("a question with no options and no free text cannot be answered")
	ErrQuestionEmpty       = errors.New("a question needs something to ask")
	ErrQuestionCrowded     = errors.New("a question offers more answers than norn will show")
	ErrQuestionUndeclared  = errors.New("a question you are not waiting on has to say what you will do meanwhile")
)

type QuestionKind string

const (
	QuestionDecision      QuestionKind = channelv1.QuestionDecision
	QuestionClarification QuestionKind = channelv1.QuestionClarification
	QuestionApproval      QuestionKind = channelv1.QuestionApproval
)

func QuestionKinds() []QuestionKind {
	return []QuestionKind{QuestionDecision, QuestionClarification, QuestionApproval}
}

func (k QuestionKind) Valid() bool {
	return slices.Contains(QuestionKinds(), k)
}

type QuestionContext struct {
	Preview   string
	Files     []string
	Artifacts []string
}

type Question struct {
	Ref           string
	Kind          QuestionKind
	Blocking      bool
	Message       string
	Options       []string
	AllowFreeText bool
	Default       string
	Wait          time.Duration
	Context       QuestionContext
	Asked         time.Time
}

func (q Question) Fault() error {
	switch {
	case strings.TrimSpace(q.Message) == "":
		return ErrQuestionEmpty
	case len(q.Options) > QuestionOptionsMax:
		return ErrQuestionCrowded
	case len(q.Options) == 0 && !q.AllowFreeText:
		return ErrQuestionUnreachable
	case !q.Blocking && strings.TrimSpace(q.Default) == "":
		return ErrQuestionUndeclared
	default:
		return nil
	}
}

type Answer struct {
	QuestionID string
	Ref        string
	Question   string
	Answer     string
	AnsweredBy string
	AnsweredAt time.Time
}

type OpenQuestion struct {
	Question Question
	Answer   *Answer
}

type AskOutcome string

const (
	AskAnswered AskOutcome = "answered"
	AskPending  AskOutcome = "pending"
	AskNoted    AskOutcome = "noted"
)

type Asked struct {
	Outcome    AskOutcome
	Ref        string
	Answer     string
	AnsweredBy string
	Advice     string
}

const (
	AskPendingAdvice = "Nobody has answered yet. Stop here rather than guessing: say what you " +
		"were about to decide and end your turn. Norn will start you again with the answer as " +
		"soon as somebody gives one."

	AskNotedAdvice = "This is recorded on the issue and you are not waiting on it. Carry on with " +
		"the default you declared."
)

func AnswerOf(answer channelv1.Answer) Answer {
	return Answer{
		QuestionID: answer.QuestionID,
		Ref:        answer.Ref,
		Question:   answer.Question,
		Answer:     answer.Answer,
		AnsweredBy: answer.AnsweredBy,
		AnsweredAt: answer.AnsweredAt,
	}
}

func AnswersInjection(answers []Answer) string {
	var said strings.Builder

	for _, answer := range answers {
		who := strings.TrimSpace(answer.AnsweredBy)
		if who == "" {
			who = "Somebody"
		}

		if question := strings.TrimSpace(answer.Question); question != "" {
			said.WriteString("You asked: " + question + "\n")
		}

		said.WriteString(
			who + " answered (question " + answer.QuestionID + "): " +
				strings.TrimSpace(answer.Answer) + "\n\n",
		)
	}

	said.WriteString("Carry on from where you left off with those decisions.")

	return said.String()
}
