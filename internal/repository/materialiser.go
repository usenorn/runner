package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=materialiser.go -destination=materialiser/mock_materialiser.go -package=materialiser -mock_names=Materialiser=MockMaterialiser

type SkipFunc func(relPath string, isDir bool) bool

type Materialised struct {
	Files    []entity.SharedFile
	Bytes    int64
	Warnings []string
}

type Materialiser interface {
	Copy(ctx context.Context, from, to string, skip SkipFunc, budget int64) (Materialised, error)
	CopyPaths(ctx context.Context, from, to string, relPaths []string) (Materialised, error)
	Remove(ctx context.Context, dir string) error
}
