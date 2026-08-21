package repository

import (
	"context"
	"crypto/ed25519"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=dashboard.go -destination=dashboard/mock_dashboard.go -package=dashboard -mock_names=Dashboard=MockDashboard

type Enrolment struct {
	Name      string
	Host      entity.Host
	PublicKey ed25519.PublicKey
}

type Enrolled struct {
	Identity     entity.Identity
	RefreshToken string
}

type Dashboard interface {
	Enrol(ctx context.Context, token string, enrolment Enrolment) (Enrolled, error)
	Exchange(
		ctx context.Context,
		refreshToken string,
		assertion entity.Assertion,
		signature string,
	) (entity.Session, error)
}
