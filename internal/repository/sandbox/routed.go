package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/bridge"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
)

type routedSandbox struct {
	runtimes map[entity.Runtime]repository.Sandbox
}

func New(
	processes repository.Process,
	ports repository.Port,
	dir *statedir.Dir,
	cfg config.Docker,
	bridged *bridge.Listener,
) repository.Sandbox {
	return &routedSandbox{runtimes: map[entity.Runtime]repository.Sandbox{
		entity.RuntimeProcess: &hostSandbox{processes: processes},
		entity.RuntimeDocker:  newDocker(processes, ports, dir, cfg, bridged),
	}}
}

func (r *routedSandbox) route(runtime entity.Runtime) (repository.Sandbox, error) {
	if runtime == "" {
		runtime = entity.RuntimeProcess
	}

	if !runtime.Valid() {
		return nil, fmt.Errorf("%w, not in %s", entity.ErrRuntimeUnsupported, runtime)
	}

	chosen, found := r.runtimes[runtime]
	if !found {
		return nil, fmt.Errorf("%w: %s", entity.ErrRuntimeUnavailable, runtime)
	}

	return chosen, nil
}

func (r *routedSandbox) Check(ctx context.Context, runtime entity.Runtime) error {
	chosen, err := r.route(runtime)
	if err != nil {
		return err
	}

	return chosen.Check(ctx, runtime)
}

func (r *routedSandbox) Open(ctx context.Context, spec entity.SandboxSpec) error {
	chosen, err := r.route(spec.Box.Runtime)
	if err != nil {
		return err
	}

	return chosen.Open(ctx, spec)
}

func (r *routedSandbox) Start(
	ctx context.Context,
	box entity.Sandbox,
	launch repository.Launch,
) (repository.Child, error) {
	chosen, err := r.route(box.Runtime)
	if err != nil {
		return nil, err
	}

	return chosen.Start(ctx, box, launch)
}

func (r *routedSandbox) Run(
	ctx context.Context,
	box entity.Sandbox,
	launch repository.Launch,
	timeout time.Duration,
) (int, error) {
	chosen, err := r.route(box.Runtime)
	if err != nil {
		return 0, err
	}

	return chosen.Run(ctx, box, launch, timeout)
}

func (r *routedSandbox) Has(ctx context.Context, box entity.Sandbox, command string) bool {
	chosen, err := r.route(box.Runtime)
	if err != nil {
		return false
	}

	return chosen.Has(ctx, box, command)
}

func (r *routedSandbox) Tools(box entity.Sandbox) (string, error) {
	chosen, err := r.route(box.Runtime)
	if err != nil {
		return "", err
	}

	return chosen.Tools(box)
}

func (r *routedSandbox) Close(ctx context.Context, box entity.Sandbox) error {
	chosen, err := r.route(box.Runtime)
	if err != nil {
		return err
	}

	return chosen.Close(ctx, box)
}

func (r *routedSandbox) Sweep(ctx context.Context) error {
	failures := make([]error, 0, len(r.runtimes))

	for _, chosen := range r.runtimes {
		failures = append(failures, chosen.Sweep(ctx))
	}

	return errors.Join(failures...)
}
