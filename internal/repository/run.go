package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=run.go -destination=run/mock_run.go -package=run -mock_names=Run=MockRun

type Run interface {
	Prepare(ctx context.Context, name string) (string, error)
	Save(ctx context.Context, snapshot entity.Snapshot) error
	Load(ctx context.Context, name string) (entity.Snapshot, error)
	List(ctx context.Context) ([]entity.Snapshot, error)
	Remove(ctx context.Context, name string) error
}
