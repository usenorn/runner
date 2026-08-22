package repository

import (
	"context"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=driver.go -destination=driver/mock_driver.go -package=driver -mock_names=Driver=MockDriver,Session=MockSession

type Session interface {
	Events() <-chan entity.DriverEvent
	Logs() <-chan string
	Reference() entity.DriverSession
	Wait() (entity.DriverResult, error)
	Stop(ctx context.Context, grace time.Duration) error
}

type Driver interface {
	Preflight(ctx context.Context, kind entity.DriverKind) entity.DriverHealth
	Start(ctx context.Context, env entity.ExecEnv, task entity.Task) (Session, error)
	Resume(
		ctx context.Context,
		env entity.ExecEnv,
		held entity.DriverSession,
		injection string,
	) (Session, error)
}
