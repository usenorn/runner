package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=inventory.go -destination=inventory/mock_inventory.go -package=inventory -mock_names=Inventory=MockInventory

type Inventory interface {
	List(ctx context.Context) ([]entity.Codebase, error)
	Load(ctx context.Context, root string) (entity.Codebase, error)
	Save(ctx context.Context, codebase entity.Codebase) error
	Remove(ctx context.Context, id uuid.UUID) error
}
