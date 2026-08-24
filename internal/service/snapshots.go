package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=snapshots.go -destination=snapshot/mock_snapshots.go -package=snapshot -mock_names=Snapshots=MockSnapshots

type TakeRequest struct {
	Path         string
	IssueKey     string
	Attempt      int
	LocalChanges entity.LocalChanges
	Base         entity.BasePolicy
	Run          string
	Branch       string
	Branches     map[string]string
}

type Snapshots interface {
	Take(ctx context.Context, request TakeRequest) (entity.Snapshot, error)
	List(ctx context.Context) ([]entity.Snapshot, error)
	Release(ctx context.Context, name string) error
	Discard(ctx context.Context, name string) error
}
