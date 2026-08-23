package entity

import (
	"errors"
	"fmt"
	"strings"
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

	if len(c.Summary)+len(c.Notes) > CompletionTextMax {
		return fmt.Errorf(
			"%w: it is %d characters and norn keeps %d",
			ErrCompleteLong, len(c.Summary)+len(c.Notes), CompletionTextMax,
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
