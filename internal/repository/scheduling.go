package repository

import "context"

//go:generate go tool mockgen -source=scheduling.go -destination=scheduling/mock_scheduling.go -package=scheduling -mock_names=Scheduling=MockScheduling

type Scheduling interface {
	Paused(ctx context.Context) (bool, error)
	Pause(ctx context.Context, paused bool) error
}
