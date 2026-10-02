package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=toolchain.go -destination=toolchain/mock_toolchain.go -package=toolchain -mock_names=Toolchain=MockToolchain

type Toolchain interface {
	Manifests(ctx context.Context, roots []entity.ManifestRoot) ([]entity.Manifest, error)
	Locate(name string) (string, bool)
}
