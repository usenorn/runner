package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=release.go -destination=release/mock_release.go -package=release -mock_names=Release=MockRelease

type Release interface {
	Latest(ctx context.Context) (entity.Release, error)
}
