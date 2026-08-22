package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=questions.go -destination=question/mock_questions.go -package=question -mock_names=Questions=MockQuestions

type Questions interface {
	Ask(ctx context.Context, executionID string, question entity.Question) (entity.Asked, error)
	Answered(ctx context.Context, executionID string, answer entity.Answer) error
	Waiting(executionID string) (entity.Question, bool)
	Take(executionID string) (entity.Question, entity.Answer, bool)
	Forget(executionID string)
}
