package repository

import "context"

//go:generate go tool mockgen -source=runtoken.go -destination=runtoken/mock_runtoken.go -package=runtoken -mock_names=RunToken=MockRunToken

type RunToken interface {
	Mint(ctx context.Context, run string) (string, error)
	Allows(ctx context.Context, run string, token string) bool
	Release(ctx context.Context, run string)
}
