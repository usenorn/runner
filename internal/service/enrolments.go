package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=enrolments.go -destination=enrolment/mock_enrolments.go -package=enrolment -mock_names=Enrolments=MockEnrolments

type ConnectInput struct {
	Token string
	Name  string
	Store entity.Store
	Force bool
}

type Connected struct {
	Identity entity.Identity
	Session  entity.SessionReport
}

type Enrolments interface {
	Current(ctx context.Context) (entity.Identity, error)
	Connect(ctx context.Context, input ConnectInput) (Connected, error)
	Disconnect(ctx context.Context) (entity.Identity, error)
}
