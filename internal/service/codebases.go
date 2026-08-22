package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=codebases.go -destination=codebase/mock_codebases.go -package=codebase -mock_names=Codebases=MockCodebases

type Scan struct {
	Inventory  entity.Inventory
	Warnings   []string
	Connected  bool
	Reconcile  bool
	CodebaseID uuid.UUID
	Drift      entity.Drift
}

type Codebases interface {
	Run(ctx context.Context)
	Scan(ctx context.Context, root string) (Scan, error)
	Accept(ctx context.Context, scan Scan) (entity.Codebase, error)
	List(ctx context.Context) ([]entity.Codebase, error)
}
