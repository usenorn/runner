package driver

import (
	"context"
	"slices"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

type claudeDriver struct {
	sandboxes repository.Sandbox
	cfg       config.Driver
	now       func() time.Time
}

func New(sandboxes repository.Sandbox, cfg config.Driver) repository.Driver {
	return &claudeDriver{sandboxes: sandboxes, cfg: cfg, now: func() time.Time {
		return time.Now().UTC()
	}}
}

func (r *claudeDriver) spawn(
	ctx context.Context,
	env entity.ExecEnv,
	args []string,
	held entity.DriverSession,
) (repository.Session, error) {
	session := newSession(held, r.now)

	if session.held.StartedAt.IsZero() {
		session.held.StartedAt = r.now()
	}

	spoken, complained := session.spoken(), session.complained()

	child, err := r.sandboxes.Start(ctx, env.Sandbox, repository.Launch{
		Dir:         env.Workspace,
		Command:     args,
		Environment: append(slices.Clone(env.Environment), tokenVariable+"="+env.AgentToken),
		Output:      spoken,
		Errors:      complained,
	})
	if err != nil {
		session.abandon()

		return nil, err
	}

	session.child = child

	go func() {
		code, err := child.Wait()

		spoken.close()
		complained.close()

		session.settle(code, err)
	}()

	return session, nil
}
