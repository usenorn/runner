package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=settings.go -destination=settings/mock_settings.go -package=settings -mock_names=Settings=MockSettings

type CodebaseSettings struct {
	GitMode      string
	Base         string
	LocalChanges string
	Fetch        *bool
}

type Settings interface {
	Load(ctx context.Context, root string) (CodebaseSettings, error)
	Plan(ctx context.Context, root string) (string, error)
	Ignores(ctx context.Context, dir string) ([]entity.IgnoreRule, error)
}
