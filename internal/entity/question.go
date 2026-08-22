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
	Answer     string
	AnsweredBy string
	AnsweredAt time.Time
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

// AnswerInjection is what a resumed session is told, and it carries the answer word for word
// because the agent branches on what a person actually decided, not on a summary of it.
func AnswerInjection(answer Answer, question string) string {
	said := answer.AnsweredBy
	if strings.TrimSpace(said) == "" {
		said = "Somebody"
	}

	return "You stopped to ask: " + strings.TrimSpace(question) + "\n\n" +
		said + " answered (question " + answer.QuestionID + "): " +
		strings.TrimSpace(answer.Answer) + "\n\n" +
		"Carry on from where you left off with that decision."
}
