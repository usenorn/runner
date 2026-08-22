package channel_test

import (
	"context"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

type runStub struct{}

func (runStub) Prepare(context.Context, string) (string, error) { return "", nil }

func (runStub) Open(context.Context, string) (string, error) { return "", nil }

func (runStub) Prune(context.Context, string) error { return nil }

func (runStub) Save(context.Context, entity.Snapshot) error { return nil }

func (runStub) Load(context.Context, string) (entity.Snapshot, error) {
	return entity.Snapshot{}, entity.ErrSnapshotMissing
}

func (runStub) List(context.Context) ([]entity.Snapshot, error) { return nil, nil }

func (runStub) Remove(context.Context, string) error { return nil }

func (runStub) SaveTask(context.Context, entity.Execution) error { return nil }

func (runStub) LoadTasks(context.Context) ([]entity.Execution, error) { return nil, nil }

func (runStub) SaveSetup(context.Context, string, entity.RunSetup) error { return nil }

func (runStub) LoadSetup(context.Context, string) (entity.RunSetup, error) {
	return entity.RunSetup{}, nil
}

func (runStub) Append(context.Context, string, entity.TimelineEntry) error { return nil }

func (runStub) Timeline(context.Context, string) ([]entity.TimelineEntry, error) {
	return nil, nil
}

type diskStub struct{}

func (diskStub) Free(context.Context, string) (int64, error) { return 100 << 30, nil }

type settingsStub struct{}

func (settingsStub) Load(context.Context, string) (repository.CodebaseSettings, error) {
	return repository.CodebaseSettings{}, nil
}

func (settingsStub) Plan(context.Context, string) (string, error) { return "", nil }

func (settingsStub) Ignores(context.Context, string) ([]entity.IgnoreRule, error) {
	return nil, nil
}

type inventoryStub struct{}

func (inventoryStub) List(context.Context) ([]entity.Codebase, error) { return nil, nil }

func (inventoryStub) Load(context.Context, string) (entity.Codebase, error) {
	return entity.Codebase{}, entity.ErrCodebaseNotConnected
}

func (inventoryStub) Save(context.Context, entity.Codebase) error { return nil }

func (inventoryStub) Remove(context.Context, uuid.UUID) error { return nil }

type snapshotStub struct{}

func (snapshotStub) Take(
	_ context.Context,
	request service.TakeRequest,
) (entity.Snapshot, error) {
	return entity.Snapshot{Name: request.Run}, nil
}

func (snapshotStub) List(context.Context) ([]entity.Snapshot, error) { return nil, nil }

func (snapshotStub) Release(context.Context, string) error { return nil }

func (snapshotStub) Discard(context.Context, string) error { return nil }
