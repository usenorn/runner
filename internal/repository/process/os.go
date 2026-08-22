package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/usenorn/runner/internal/pkg/proc"
	"github.com/usenorn/runner/internal/repository"
)

const (
	killed      = -1
	identifying = 2 * time.Second
	recognising = time.Minute
	pollDelay   = 20 * time.Millisecond
)

type osProcess struct{}

func New() repository.Process {
	return &osProcess{}
}

func (r *osProcess) Start(_ context.Context, launch repository.Launch) (repository.Child, error) {
	command := exec.Command(launch.Command[0], launch.Command[1:]...)

	dress(command, launch)
	proc.Contain(command)

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", launch.Command[0], err)
	}

	held := &osChild{command: command, done: make(chan struct{})}

	go func() {
		held.err = command.Wait()
		held.code = command.ProcessState.ExitCode()

		close(held.done)
	}()

	return held, nil
}

func (r *osProcess) Run(
	ctx context.Context,
	launch repository.Launch,
	timeout time.Duration,
) (int, error) {
	if timeout > 0 {
		var stop context.CancelFunc

		ctx, stop = context.WithTimeout(ctx, timeout)
		defer stop()
	}

	command := exec.CommandContext(ctx, launch.Command[0], launch.Command[1:]...)

	dress(command, launch)
	proc.Stoppable(command)

	err := command.Run()

	if ctx.Err() != nil {
		return killed, fmt.Errorf("run %s: %w", launch.Command[0], ctx.Err())
	}

	var exit *exec.ExitError

	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}

	if err != nil {
		return killed, fmt.Errorf("run %s: %w", launch.Command[0], err)
	}

	return 0, nil
}

func (r *osProcess) Stray(ctx context.Context, pid int, startedAt time.Time) bool {
	if pid <= 0 || startedAt.IsZero() || !proc.Alive(pid) {
		return false
	}

	ctx, stop := context.WithTimeout(ctx, identifying)
	defer stop()

	out, err := exec.CommandContext(
		ctx, "ps", "-p", strconv.Itoa(pid), "-o", "pgid=,etime=",
	).Output()
	if err != nil {
		return false
	}

	group, elapsed, found := strings.Cut(strings.TrimSpace(string(out)), " ")
	if !found {
		return false
	}

	if leader, err := strconv.Atoi(strings.TrimSpace(group)); err != nil || leader != pid {
		return false
	}

	since, err := lasting(strings.TrimSpace(elapsed))
	if err != nil {
		return false
	}

	drift := time.Since(startedAt.Add(since))

	return drift > -recognising && drift < recognising
}

func lasting(elapsed string) (time.Duration, error) {
	days := 0

	if before, after, found := strings.Cut(elapsed, "-"); found {
		counted, err := strconv.Atoi(before)
		if err != nil {
			return 0, fmt.Errorf("read how long a process has been running: %w", err)
		}

		days, elapsed = counted, after
	}

	parts := strings.Split(elapsed, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("read how long a process has been running: %s", elapsed)
	}

	held := time.Duration(days) * 24 * time.Hour

	for _, unit := range []time.Duration{time.Second, time.Minute, time.Hour} {
		if len(parts) == 0 {
			break
		}

		counted, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			return 0, fmt.Errorf("read how long a process has been running: %w", err)
		}

		held += time.Duration(counted) * unit
		parts = parts[:len(parts)-1]
	}

	return held, nil
}

func (r *osProcess) Sweep(ctx context.Context, pid int, grace time.Duration) error {
	if pid <= 0 {
		return nil
	}

	if err := proc.Terminate(pid); err != nil {
		return fmt.Errorf("ask the group led by %d to stop: %w", pid, err)
	}

	return settle(ctx, pid, grace)
}

func settle(ctx context.Context, pid int, grace time.Duration) error {
	deadline, stop := context.WithTimeout(ctx, grace)
	defer stop()

	for proc.Alive(pid) {
		select {
		case <-deadline.Done():
			if err := proc.Kill(pid); err != nil {
				return fmt.Errorf("stop the group led by %d: %w", pid, err)
			}

			return nil
		case <-time.After(pollDelay):
		}
	}

	return nil
}

func dress(command *exec.Cmd, launch repository.Launch) {
	command.Dir = launch.Dir
	command.Env = launch.Environment
	command.Stdout = launch.Output
	command.Stderr = launch.Output
}

type osChild struct {
	command *exec.Cmd
	done    chan struct{}
	code    int
	err     error
	once    sync.Once
}

func (c *osChild) PID() int {
	return c.command.Process.Pid
}

func (c *osChild) Wait() (int, error) {
	<-c.done

	var exit *exec.ExitError

	if errors.As(c.err, &exit) || c.err == nil {
		return c.code, nil
	}

	return c.code, fmt.Errorf("wait for %s: %w", c.command.Path, c.err)
}

func (c *osChild) Stop(ctx context.Context, grace time.Duration) error {
	c.once.Do(func() { _ = proc.Terminate(c.PID()) })

	deadline, stop := context.WithTimeout(ctx, grace)
	defer stop()

	select {
	case <-c.done:
		return nil
	case <-deadline.Done():
	}

	if err := proc.Kill(c.PID()); err != nil {
		return fmt.Errorf("stop %s: %w", c.command.Path, err)
	}

	<-c.done

	return nil
}
