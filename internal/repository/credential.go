package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=credential.go -destination=credential/mock_credential.go -package=credential -mock_names=Credential=MockCredential

type Credential interface {
	Usable(ctx context.Context, store entity.Store) error
	Load(ctx context.Context, store entity.Store) (entity.Credentials, error)
	Save(ctx context.Context, store entity.Store, credentials entity.Credentials) error
	Clear(ctx context.Context) error
}
