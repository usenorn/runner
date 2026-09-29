package repository

import (
	"context"
	"io"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=upload.go -destination=upload/mock_upload.go -package=upload -mock_names=Upload=MockUpload

type Upload interface {
	PublishArtifact(
		ctx context.Context,
		token string,
		executionID string,
		label string,
		body io.Reader,
	) (entity.ArtifactReceipt, error)
}
