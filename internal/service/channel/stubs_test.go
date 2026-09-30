package channel_test

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

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

func (runStub) Retire(context.Context, string) error { return nil }

func (runStub) Usage(context.Context) ([]entity.RunUsage, error) { return nil, nil }

func (runStub) SaveTask(context.Context, entity.Execution) error { return nil }

func (runStub) LoadTask(context.Context, string) (entity.Execution, error) {
	return entity.Execution{}, entity.ErrExecutionUnknown
}

func (runStub) LoadTasks(context.Context) ([]entity.Execution, error) { return nil, nil }

func (runStub) SaveSetup(context.Context, string, entity.RunSetup) error { return nil }

func (runStub) LoadSetup(context.Context, string) (entity.RunSetup, error) {
	return entity.RunSetup{}, nil
}

func (runStub) SaveDriver(context.Context, string, entity.RunDriver) error { return nil }

func (runStub) LoadDriver(context.Context, string) (entity.RunDriver, error) {
	return entity.RunDriver{}, nil
}

func (runStub) SaveServices(context.Context, string, entity.RunServices) error { return nil }

func (runStub) SaveToolkit(context.Context, string, entity.Toolkit) error { return nil }

func (runStub) LoadToolkit(context.Context, string) (entity.Toolkit, error) {
	return entity.Toolkit{}, nil
}

func (runStub) LoadServices(context.Context, string) (entity.RunServices, error) {
	return entity.RunServices{}, nil
}

func (runStub) Append(context.Context, string, entity.TimelineEntry) error { return nil }

func (runStub) RecordTranscript(context.Context, string, entity.DriverEvent) error { return nil }

func (runStub) RecordStderr(context.Context, string, string) error { return nil }

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

func (settingsStub) Definition(context.Context, string) (entity.PlanDefinition, error) {
	return entity.PlanDefinition{}, nil
}

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

type servicesStub struct{}

func (servicesStub) Run(context.Context) {}

func (servicesStub) Start(
	context.Context,
	string,
	entity.Service,
) (entity.ServiceRecord, error) {
	return entity.ServiceRecord{}, nil
}

func (servicesStub) Await(context.Context, string, string) (entity.ServiceRecord, error) {
	return entity.ServiceRecord{}, nil
}

func (servicesStub) Stop(context.Context, string, string) (entity.ServiceRecord, error) {
	return entity.ServiceRecord{}, nil
}

func (servicesStub) Restart(context.Context, string, string) (entity.ServiceRecord, error) {
	return entity.ServiceRecord{}, nil
}

func (servicesStub) List(context.Context, string) ([]entity.ServiceRecord, error) {
	return nil, nil
}

func (servicesStub) Logs(
	context.Context,
	string,
	string,
	entity.LogQuery,
) ([]string, error) {
	return nil, nil
}

func (servicesStub) Step(context.Context, string, entity.Step) (entity.StepResult, error) {
	return entity.StepResult{}, nil
}

func (servicesStub) Port(context.Context, string, string) (int, error) { return 0, nil }

func (servicesStub) Release(context.Context, string) error { return nil }

type uploadStub struct{}

func (uploadStub) Publish(
	context.Context,
	string,
	entity.Artifact,
) (entity.ArtifactReceipt, error) {
	return entity.ArtifactReceipt{}, nil
}

type toolkitStub struct{}

func (toolkitStub) InstallSkill(context.Context, entity.ToolkitSkill, string) error { return nil }

func (toolkitStub) Installed(string) bool { return true }

func (toolkitStub) ReachNorn(context.Context, string) error { return nil }

func (toolkitStub) NornHandler(accessToken string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(accessToken))
	})
}

type driverStub struct{}

func (driverStub) Preflight(context.Context, entity.DriverKind, string) entity.DriverHealth {
	return entity.DriverHealth{Kind: entity.DriverClaude, Installed: true, SignedIn: true}
}

func (driverStub) Start(
	context.Context,
	entity.ExecEnv,
	entity.Task,
) (repository.Session, error) {
	return nil, entity.ErrDriverMissing
}

func (driverStub) Resume(
	context.Context,
	entity.ExecEnv,
	entity.DriverSession,
	string,
) (repository.Session, error) {
	return nil, entity.ErrDriverMissing
}

func (uploadStub) Attach(
	context.Context,
	string,
	string,
	[]byte,
) (entity.ArtifactReceipt, error) {
	return entity.ArtifactReceipt{}, nil
}

type schedulingStub struct{}

func (schedulingStub) Paused(context.Context) (bool, error) { return false, nil }

func (schedulingStub) Pause(context.Context, bool) error { return nil }

func (runStub) SaveQuestion(context.Context, string, entity.OpenQuestion) error { return nil }

func (runStub) LoadQuestion(context.Context, string) (entity.OpenQuestion, error) {
	return entity.OpenQuestion{}, entity.ErrSnapshotMissing
}

func (runStub) ClearQuestion(context.Context, string) error { return nil }

func (runStub) LatestPlan(context.Context, string) (string, error) { return "", nil }

func (runStub) SaveResume(context.Context, string, channelv1.Instruction) error { return nil }

func (runStub) LoadResume(context.Context, string) (channelv1.Instruction, error) {
	return channelv1.Instruction{}, entity.ErrSnapshotMissing
}

func (runStub) ClearResume(context.Context, string) error { return nil }

func (runStub) SaveReview(context.Context, string, entity.Review) error { return nil }

func (runStub) LoadReview(context.Context, string) (entity.Review, error) {
	return entity.Review{}, entity.ErrReviewMissing
}

func (runStub) SavePublication(context.Context, string, entity.Publication) error { return nil }

func (runStub) LoadPublication(context.Context, string) (entity.Publication, error) {
	return entity.Publication{}, nil
}
