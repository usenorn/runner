package repository

import (
	"context"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

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
	SaveToolkit(ctx context.Context, name string, toolkit entity.Toolkit) error
	LoadToolkit(ctx context.Context, name string) (entity.Toolkit, error)
	SaveQuestion(ctx context.Context, name string, open entity.OpenQuestion) error
	LoadQuestion(ctx context.Context, name string) (entity.OpenQuestion, error)
	ClearQuestion(ctx context.Context, name string) error
	LatestPlan(ctx context.Context, name string) (string, error)
	SaveResume(ctx context.Context, name string, instruction channelv1.Instruction) error
	LoadResume(ctx context.Context, name string) (channelv1.Instruction, error)
	ClearResume(ctx context.Context, name string) error
	Append(ctx context.Context, name string, entry entity.TimelineEntry) error
	RecordTranscript(ctx context.Context, name string, event entity.DriverEvent) error
	RecordStderr(ctx context.Context, name string, line string) error
	Timeline(ctx context.Context, name string) ([]entity.TimelineEntry, error)
}
