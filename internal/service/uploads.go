package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=uploads.go -destination=upload/mock_uploads.go -package=upload -mock_names=Uploads=MockUploads

type Uploads interface {
	Run(ctx context.Context)
	Open(ctx context.Context, executionID string) (entity.TelemetryMode, error)
	Event(ctx context.Context, executionID string, event entity.DriverEvent)
	Line(ctx context.Context, executionID string, line entity.LogLine)
	Flush(ctx context.Context, executionID string) error
	Close(ctx context.Context, executionID string)
	Publish(
		ctx context.Context,
		executionID string,
		artifact entity.Artifact,
	) (entity.ArtifactReceipt, error)
}
