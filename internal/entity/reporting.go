package entity

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	ProgressSummaryMax = 500
	CompletionTextMax  = 4000
)

const CompletedAdvice = "Recorded. Say nothing further and end your turn; norn takes it from " +
	"here and collects the work on the branches you committed to."

var (
	ErrProgressEmpty = errors.New("a progress report needs something to say")
	ErrProgressRange = errors.New("progress is a percentage, so it lies between 0 and 100")
	ErrCompleteEmpty = errors.New("finishing needs a summary of what changed")
	ErrCompleteLong  = errors.New("that summary is longer than norn keeps")
	ErrReplyEmpty    = errors.New("a reply to a review thread needs something to say")
	ErrReplyLong     = errors.New("that reply is longer than a review comment can be")
	ErrReplyUnasked  = errors.New(
		"that is not a review thread this run was asked to answer; use a thread id from the " +
			"review feedback",
	)
)

type Progress struct {
	Summary string
	Phase   string
	Percent int
}

func (p Progress) Valid() error {
	if strings.TrimSpace(p.Summary) == "" {
		return ErrProgressEmpty
	}

	if p.Percent < 0 || p.Percent > 100 {
		return fmt.Errorf("%w: %d is not", ErrProgressRange, p.Percent)
	}

	return nil
}

func (p Progress) Line() string {
	summary := fit(strings.TrimSpace(p.Summary), ProgressSummaryMax)

	if phase := strings.TrimSpace(p.Phase); phase != "" {
		return phase + ": " + summary
	}

	return summary
}

type Completion struct {
	Summary string
	Notes   string
}

func (c Completion) Valid() error {
	if strings.TrimSpace(c.Summary) == "" {
		return ErrCompleteEmpty
	}

	if length := utf8.RuneCountInString(c.Summary) + utf8.RuneCountInString(c.Notes); length > CompletionTextMax {
		return fmt.Errorf(
			"%w: it is %d characters and norn keeps %d",
			ErrCompleteLong, length, CompletionTextMax,
		)
	}

	return nil
}

func (c Completion) Line() string {
	summary := strings.TrimSpace(c.Summary)

	if notes := strings.TrimSpace(c.Notes); notes != "" {
		return summary + "\n\nFor whoever reviews this: " + notes
	}

	return summary
}

const ReviewReplyMax = 4000

type ReviewReply struct {
	CommentID string
	Body      string
}

func (r ReviewReply) Valid() error {
	body := strings.TrimSpace(r.Body)

	if body == "" {
		return ErrReplyEmpty
	}

	if utf8.RuneCountInString(body) > ReviewReplyMax {
		return fmt.Errorf("%w: it is %d characters and a comment holds %d",
			ErrReplyLong, utf8.RuneCountInString(body), ReviewReplyMax)
	}

	return nil
}

func CompletionLimits() string {
	return fmt.Sprintf(
		"The summary and the notes together hold at most %d characters; put anything longer in "+
			"a file and publish_artifact it.",
		CompletionTextMax,
	)
}

func ProgressLimits() string {
	return fmt.Sprintf("Only the first %d characters of a summary are kept.", ProgressSummaryMax)
}

func ReplyLimits() string {
	return fmt.Sprintf("A reply holds at most %d characters.", ReviewReplyMax)
}
