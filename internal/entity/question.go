package entity

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

var (
	ErrQuestionUnknownRun = errors.New("this machine is not running that execution")
	ErrQuestionEmpty      = errors.New("a question needs something to ask")
	ErrQuestionCrowded    = errors.New("a question offers more answers than norn will show")
	ErrQuestionUndeclared = errors.New("a question you are not waiting on has to say what you will do meanwhile")
	ErrQuestionTooLong    = errors.New("a question is longer than norn will show")
	ErrQuestionOption     = errors.New("an answer a question offers cannot be shown")
	ErrQuestionDefault    = errors.New("what you will do meanwhile is longer than norn keeps")
	ErrQuestionContext    = errors.New("a question points at more files or artifacts than norn keeps")
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
	Ref      string
	Kind     QuestionKind
	Blocking bool
	Message  string
	Options  []string
	Default  string
	Wait     time.Duration
	Context  QuestionContext
	Asked    time.Time
}

func (q Question) Fault() error {
	switch {
	case strings.TrimSpace(q.Message) == "":
		return ErrQuestionEmpty
	case runes(q.Message) > channelv1.QuestionTextMax:
		return fmt.Errorf(
			"%w: it is %d characters and norn takes at most %d; say it more briefly",
			ErrQuestionTooLong, runes(q.Message), channelv1.QuestionTextMax,
		)
	case len(q.Options) > channelv1.QuestionOptionsMax:
		return fmt.Errorf("%w: offer at most %d", ErrQuestionCrowded, channelv1.QuestionOptionsMax)
	case !q.Blocking && strings.TrimSpace(q.Default) == "":
		return ErrQuestionUndeclared
	case runes(q.Default) > channelv1.QuestionDefaultMax:
		return fmt.Errorf(
			"%w: it is %d characters and norn takes at most %d",
			ErrQuestionDefault, runes(q.Default), channelv1.QuestionDefaultMax,
		)
	case len(q.Context.Files) > channelv1.QuestionContextFilesMax ||
		len(q.Context.Artifacts) > channelv1.QuestionContextFilesMax:
		return fmt.Errorf(
			"%w: name at most %d files and %d artifacts",
			ErrQuestionContext, channelv1.QuestionContextFilesMax, channelv1.QuestionContextFilesMax,
		)
	default:
		return q.optionFault()
	}
}

func (q Question) optionFault() error {
	for index, option := range q.Options {
		switch length := runes(option); {
		case length == 0:
			return fmt.Errorf("%w: option %d is empty", ErrQuestionOption, index+1)
		case length > channelv1.QuestionOptionMax:
			return fmt.Errorf(
				"%w: option %d is %d characters and norn takes at most %d; shorten it and put the detail "+
					"in the question",
				ErrQuestionOption, index+1, length, channelv1.QuestionOptionMax,
			)
		}
	}

	return nil
}

func runes(text string) int {
	return utf8.RuneCountInString(strings.TrimSpace(text))
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

const (
	ResumeRefused = "refused"
	NornAnswerer  = "norn"
)

func RefusedQuestionAnswer(ref, reason string) Answer {
	return Answer{Ref: ref, AnsweredBy: NornAnswerer, Answer: RefusedQuestionInjection("", reason)}
}

func RefusedQuestionInjection(question, reason string) string {
	var said strings.Builder

	if asked := strings.TrimSpace(question); asked != "" {
		said.WriteString("You asked: " + asked + "\n\n")
	}

	fmt.Fprintf(&said,
		"Norn could not take that question, so it never reached a person: %s.\n\n"+
			"Ask it again within norn's limits: the question at most %d characters, at most %d "+
			"options, each at most %d characters. Put the detail in the question rather than in "+
			"the options. If you can decide without a person, carry on with your best judgement "+
			"and say what you chose.",
		strings.TrimSpace(reason), channelv1.QuestionTextMax, channelv1.QuestionOptionsMax,
		channelv1.QuestionOptionMax,
	)

	return said.String()
}

func RefusalNote(kind channelv1.MessageType, reason string) string {
	if kind == channelv1.QuestionAsked {
		return "norn could not take the question the coding agent asked: " + strings.TrimSpace(reason)
	}

	return fmt.Sprintf("norn refused a %s this machine sent: %s", kind, strings.TrimSpace(reason))
}
