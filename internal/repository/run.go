package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=run.go -destination=run/mock_run.go -package=run -mock_names=Run=MockRun

type Run interface {
	Prepare(ctx context.Context, name string) (string, error)
	Open(ctx context.Context, name string) (string, error)
	Save(ctx context.Context, snapshot entity.Snapshot) error
	Load(ctx context.Context, name string) (entity.Snapshot, error)
	List(ctx context.Context) ([]entity.Snapshot, error)
	Usage(ctx context.Context) ([]entity.RunUsage, error)
	Remove(ctx context.Context, name string) error
	Retire(ctx context.Context, name string) error
	Prune(ctx context.Context, name string) error
	SaveTask(ctx context.Context, execution entity.Execution) error
	LoadTask(ctx context.Context, name string) (entity.Execution, error)
	LoadTasks(ctx context.Context) ([]entity.Execution, error)
	SaveSetup(ctx context.Context, name string, setup entity.RunSetup) error
	LoadSetup(ctx context.Context, name string) (entity.RunSetup, error)
	SaveDriver(ctx context.Context, name string, driver entity.RunDriver) error
	LoadDriver(ctx context.Context, name string) (entity.RunDriver, error)
	SaveServices(ctx context.Context, name string, services entity.RunServices) error
	LoadServices(ctx context.Context, name string) (entity.RunServices, error)
	Append(ctx context.Context, name string, entry entity.TimelineEntry) error
	Timeline(ctx context.Context, name string) ([]entity.TimelineEntry, error)
}
