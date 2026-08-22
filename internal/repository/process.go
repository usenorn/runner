package repository

import (
	"context"
	"io"
	"time"
)

//go:generate go tool mockgen -source=process.go -destination=process/mock_process.go -package=process -mock_names=Process=MockProcess,Child=MockChild

type Launch struct {
	Dir         string
	Command     []string
	Environment []string
	Output      io.Writer
}

type Child interface {
	PID() int
	Wait() (int, error)
	Stop(ctx context.Context, grace time.Duration) error
}

type Process interface {
	Start(ctx context.Context, launch Launch) (Child, error)
	Run(ctx context.Context, launch Launch, timeout time.Duration) (int, error)
	Stray(ctx context.Context, pid int, startedAt time.Time) bool
	Sweep(ctx context.Context, pid int, grace time.Duration) error
}
