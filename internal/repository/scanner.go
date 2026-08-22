package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=scanner.go -destination=scanner/mock_scanner.go -package=scanner -mock_names=Scanner=MockScanner

type ScannedFolder struct {
	Root         string
	Repositories []entity.GitFacts
	SharedFiles  []string
	Warnings     []string
}

type Scanner interface {
	Scan(ctx context.Context, root string, depth int) (ScannedFolder, error)
}
