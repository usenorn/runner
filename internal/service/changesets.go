package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=changesets.go -destination=changeset/mock_changesets.go -package=changeset -mock_names=ChangeSets=MockChangeSets

type ChangeSets interface {
	Uncommitted(ctx context.Context, snapshot entity.Snapshot) ([]entity.UncommittedWork, error)
	Publish(
		ctx context.Context,
		execution entity.Execution,
		snapshot entity.Snapshot,
		completion entity.Completion,
		previews []entity.PreviewLink,
	) (entity.ChangeSet, error)
}
