package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=previews.go -destination=preview/mock_previews.go -package=preview -mock_names=Previews=MockPreviews

type Previews interface {
	Expose(
		ctx context.Context,
		executionID string,
		wanted entity.Preview,
	) (entity.Preview, error)
	Close(ctx context.Context, executionID string, name string) (entity.Preview, error)
	List(ctx context.Context, executionID string) ([]entity.Preview, error)
	Release(ctx context.Context, executionID string)
}
