package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=forge.go -destination=forge/mock_forge.go -package=forge -mock_names=Forge=MockForge

type Forge interface {
	Available(ctx context.Context, dir string) (entity.ForgeKind, bool)
	Existing(ctx context.Context, dir, branch string) (string, error)
	Open(ctx context.Context, dir string, request entity.PullRequest) (string, error)
	Status(ctx context.Context, dir, address string) (entity.PullRequestStatus, error)
	FailedLog(ctx context.Context, dir string, check entity.FailedCheck) (string, error)
	Reply(ctx context.Context, dir string, thread entity.PullRequestThread, body string) (entity.PullRequestComment, error)
}
