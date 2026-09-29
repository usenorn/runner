package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=uploads.go -destination=upload/mock_uploads.go -package=upload -mock_names=Uploads=MockUploads

type Uploads interface {
	Publish(
		ctx context.Context,
		executionID string,
		artifact entity.Artifact,
	) (entity.ArtifactReceipt, error)
	Attach(
		ctx context.Context,
		executionID string,
		label string,
		body []byte,
	) (entity.ArtifactReceipt, error)
}
