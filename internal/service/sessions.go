package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=sessions.go -destination=session/mock_sessions.go -package=session -mock_names=Sessions=MockSessions

type Sessions interface {
	Run(ctx context.Context)
	Access(ctx context.Context) (string, error)
	Ticket(ctx context.Context) (string, error)
	TunnelTicket(ctx context.Context) (entity.TunnelTicket, error)
	Previews() entity.PreviewService
	Report() entity.SessionReport
	Adopt(ctx context.Context, identity entity.Identity) entity.SessionReport
	Forget()
}
