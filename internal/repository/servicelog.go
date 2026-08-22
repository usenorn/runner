package repository

import (
	"context"
	"io"
)

//go:generate go tool mockgen -source=servicelog.go -destination=servicelog/mock_servicelog.go -package=servicelog -mock_names=ServiceLog=MockServiceLog

type ServiceLog interface {
	Open(ctx context.Context, run string, name string) (io.WriteCloser, error)
	Tail(ctx context.Context, run string, name string, lines int) ([]string, error)
}
