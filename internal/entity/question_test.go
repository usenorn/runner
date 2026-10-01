package entity_test

import (
	"errors"
	"strings"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func TestAQuestionNornWouldRefuseIsTurnedBackBeforeItLeavesTheMachine(t *testing.T) {
	asked := func(change func(*entity.Question)) entity.Question {
		question := entity.Question{
			Kind: entity.QuestionDecision, Blocking: true,
			Message: "Keep the old endpoint?", Options: []string{"Keep it", "Remove it"},
		}
		change(&question)

		return question
	}

	for name, tc := range map[string]struct {
		question entity.Question
		want     error
		says     string
	}{
		"within every limit": {asked(func(*entity.Question) {}), nil, ""},
		"an option exactly at the limit": {
			asked(func(q *entity.Question) { q.Options[1] = strings.Repeat("é", channelv1.QuestionOptionMax) }), nil, "",
		},
		"an option one character over": {
			asked(func(q *entity.Question) { q.Options[1] = strings.Repeat("é", channelv1.QuestionOptionMax+1) }),
			entity.ErrQuestionOption, "option 2 is 201 characters and norn takes at most 200",
		},
		"an empty option": {
			asked(func(q *entity.Question) { q.Options[0] = "  " }), entity.ErrQuestionOption, "option 1 is empty",
		},
		"too many options": {
			asked(func(q *entity.Question) { q.Options = make([]string, channelv1.QuestionOptionsMax+1) }),
			entity.ErrQuestionCrowded, "at most 8",
		},
		"a question too long to show": {
			asked(func(q *entity.Question) { q.Message = strings.Repeat("x", channelv1.QuestionTextMax+1) }),
			entity.ErrQuestionTooLong, "at most 1000",
		},
		"a meanwhile too long to keep": {
			asked(func(q *entity.Question) {
				q.Blocking = false
				q.Default = strings.Repeat("x", channelv1.QuestionDefaultMax+1)
			}),
			entity.ErrQuestionDefault, "at most 2000",
		},
		"too many files": {
			asked(func(q *entity.Question) { q.Context.Files = make([]string, channelv1.QuestionContextFilesMax+1) }),
			entity.ErrQuestionContext, "at most 20 files",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.question.Fault()
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("Fault = %v, want %v", err, tc.want)
			}

			if tc.says != "" && !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("the agent would be told %q, which does not say %q", err, tc.says)
			}
		})
	}
}
