package supervisor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

func (s *servicesSupervisor) Step(
	ctx context.Context,
	executionID string,
	step entity.Step,
) (entity.StepResult, error) {
	if err := step.Valid(); err != nil {
		return entity.StepResult{}, err
	}

	execution, err := s.claim(ctx, executionID)
	if err != nil {
		return entity.StepResult{}, err
	}

	ports, err := s.ports.Held(ctx, executionID)
	if err != nil {
		return entity.StepResult{}, err
	}

	command, dir, err := unfold(step, ports)
	if err != nil {
		return entity.StepResult{}, err
	}

	sink, err := s.logs.Open(ctx, executionID, step.Name)
	if err != nil {
		return entity.StepResult{}, err
	}

	watched := newStream(sink)

	defer func() { _ = watched.Close() }()

	timeout := step.Timeout
	if timeout <= 0 {
		timeout = s.cfg.StepTimeout
	}

	s.tell(ctx, executionID, running(step.Name, command))

	began := s.now()

	code, err := s.processes.Run(ctx, repository.Launch{
		Dir:         filepath.Join(execution.Directory, entity.RunWorkspaceDir, dir),
		Command:     command,
		Environment: environment(execution, entity.Service{}, ports),
		Output:      watched,
	}, timeout)

	result := entity.StepResult{
		Name:     step.Name,
		ExitCode: code,
		Output:   strings.Join(watched.Recent(entity.StepTailLines), "\n"),
		Took:     s.now().Sub(began),
	}

	if errors.Is(err, context.DeadlineExceeded) {
		result.TimedOut = true

		s.tell(ctx, executionID, overran(step.Name, timeout))

		return result, fmt.Errorf("%w: %s ran for %s", entity.ErrStepTimedOut, step.Name, timeout)
	}

	if err != nil {
		return result, err
	}

	s.tell(ctx, executionID, finished(step.Name, code, result.Took))

	return result, nil
}

func unfold(step entity.Step, ports map[string]int) ([]string, string, error) {
	command := make([]string, 0, len(step.Command))

	for _, argument := range step.Command {
		filled, err := entity.ResolvePorts(argument, ports)
		if err != nil {
			return nil, "", err
		}

		command = append(command, filled)
	}

	dir, err := entity.ResolvePorts(step.Dir, ports)
	if err != nil {
		return nil, "", err
	}

	return command, dir, nil
}

func running(name string, command []string) string {
	return fmt.Sprintf("the step %s is running %s", name, strings.Join(command, " "))
}

func finished(name string, code int, took time.Duration) string {
	if code == 0 {
		return fmt.Sprintf("the step %s finished in %s", name, took.Round(time.Millisecond))
	}

	return fmt.Sprintf(
		"the step %s stopped with exit code %d after %s", name, code, took.Round(time.Millisecond),
	)
}

func overran(name string, timeout time.Duration) string {
	return fmt.Sprintf(
		"the step %s was given %s to finish, did not, and was stopped along with everything it "+
			"had started",
		name, timeout,
	)
}
