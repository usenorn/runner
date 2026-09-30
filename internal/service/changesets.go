package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=changesets.go -destination=changeset/mock_changesets.go -package=changeset -mock_names=ChangeSets=MockChangeSets

type ChangeSets interface {
	Uncommitted(ctx context.Context, snapshot entity.Snapshot) ([]entity.UncommittedWork, error)
	Collect(
		ctx context.Context,
		execution entity.Execution,
		snapshot entity.Snapshot,
		completion entity.Completion,
		pass entity.ReviewPass,
	) (entity.ChangeSet, error)
	Tips(ctx context.Context, review entity.Review) (map[string]string, error)
	Publish(
		ctx context.Context,
		execution entity.Execution,
		review entity.Review,
	) (entity.Publication, error)
}
