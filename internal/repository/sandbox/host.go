package sandbox

import (
	"context"
	"os/exec"
	"time"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

type hostSandbox struct {
	processes repository.Process
}

func (r *hostSandbox) Check(context.Context, entity.Runtime) error {
	return nil
}

func (r *hostSandbox) Open(context.Context, entity.SandboxSpec) error {
	return nil
}

func (r *hostSandbox) Start(
	ctx context.Context,
	_ entity.Sandbox,
	launch repository.Launch,
) (repository.Child, error) {
	return r.processes.Start(ctx, launch)
}

func (r *hostSandbox) Run(
	ctx context.Context,
	_ entity.Sandbox,
	launch repository.Launch,
	timeout time.Duration,
) (int, error) {
	return r.processes.Run(ctx, launch, timeout)
}

func (r *hostSandbox) Has(_ context.Context, _ entity.Sandbox, command string) bool {
	_, err := exec.LookPath(command)

	return err == nil
}

func (r *hostSandbox) Tools(entity.Sandbox) (string, error) {
	return "", nil
}

func (r *hostSandbox) Close(context.Context, entity.Sandbox) error {
	return nil
}

func (r *hostSandbox) Sweep(context.Context) error {
	return nil
}
