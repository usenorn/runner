package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=identity.go -destination=identity/mock_identity.go -package=identity -mock_names=Identity=MockIdentity

type Identity interface {
	Load(ctx context.Context) (entity.Identity, error)
	Save(ctx context.Context, identity entity.Identity) error
	Clear(ctx context.Context) error
}
