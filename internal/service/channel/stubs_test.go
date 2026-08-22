package channel_test

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

type runStub struct{}

func (runStub) Prepare(context.Context, string) (string, error) { return "", nil }

func (runStub) Save(context.Context, entity.Snapshot) error { return nil }

func (runStub) Load(context.Context, string) (entity.Snapshot, error) {
	return entity.Snapshot{}, entity.ErrSnapshotMissing
}

func (runStub) List(context.Context) ([]entity.Snapshot, error) { return nil, nil }

func (runStub) Remove(context.Context, string) error { return nil }

func (runStub) SaveTask(context.Context, entity.Execution) error { return nil }

func (runStub) LoadTasks(context.Context) ([]entity.Execution, error) { return nil, nil }

type diskStub struct{}

func (diskStub) Free(context.Context, string) (int64, error) { return 100 << 30, nil }
