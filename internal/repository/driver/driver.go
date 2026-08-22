package driver

import (
	"context"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

type claudeDriver struct {
	processes repository.Process
	cfg       config.Driver
	now       func() time.Time
}

func New(processes repository.Process, cfg config.Driver) repository.Driver {
	return &claudeDriver{processes: processes, cfg: cfg, now: func() time.Time {
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

	child, err := r.processes.Start(ctx, repository.Launch{
		Dir:         env.Workspace,
		Command:     args,
		Environment: env.Environment,
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
