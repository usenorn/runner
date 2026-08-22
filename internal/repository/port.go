package repository

import "context"

//go:generate go tool mockgen -source=port.go -destination=port/mock_port.go -package=port -mock_names=Port=MockPort

type Port interface {
	Reserve(ctx context.Context, run string, name string) (int, error)
	Held(ctx context.Context, run string) (map[string]int, error)
	Release(ctx context.Context, run string)
}
