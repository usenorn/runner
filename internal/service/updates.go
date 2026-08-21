package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=updates.go -destination=update/mock_updates.go -package=update -mock_names=Updates=MockUpdates

type Updates interface {
	Run(ctx context.Context)
	Report() entity.Update
	Check(ctx context.Context) entity.Update
}
