package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=toolkit.go -destination=toolkit/mock_toolkit.go -package=toolkit -mock_names=Toolkit=MockToolkit

type Toolkit interface {
	InstallSkill(ctx context.Context, skill entity.ToolkitSkill, plugin string) error
	Installed(command string) bool
	ReachNorn(ctx context.Context, accessToken string) error
}
