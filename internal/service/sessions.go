package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=sessions.go -destination=session/mock_sessions.go -package=session -mock_names=Sessions=MockSessions

type Sessions interface {
	Run(ctx context.Context)
	Report() entity.SessionReport
	Adopt(ctx context.Context, identity entity.Identity) entity.SessionReport
	Forget()
}
