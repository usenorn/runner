package supervisor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func (s *servicesSupervisor) watch(
	base context.Context,
	execution entity.Execution,
	entry *supervised,
	listening *heard,
) {
	defer close(entry.done)

	ctx, stop := context.WithCancel(base)
	defer stop()

	s.mu.Lock()
	entry.stop = stop
	s.mu.Unlock()

	for {
		code := s.attend(ctx, execution, entry, listening)

		if s.expected(entry) {
			s.settle(ctx, execution.ID, entry, entity.ServiceStopped, s.why(entry))

			return
		}

		if ctx.Err() != nil {
			s.settle(ctx, execution.ID, entry, entity.ServiceStopped, drainingNote)

			return
		}

		s.mu.Lock()
		entry.record.Attempts++
		attempt := entry.record.Attempts
		s.mu.Unlock()

		if attempt > s.cfg.RestartAttempts {
			s.gone(entry)
			s.settle(ctx, execution.ID, entry, entity.ServiceUnhealthy, gaveUp(code, attempt-1))

			return
		}

		waiting := s.backoff(attempt)

		s.tell(ctx, execution.ID, retrying(entry.record.Name, code, attempt, s.cfg.RestartAttempts, waiting))

		select {
		case <-ctx.Done():
			s.settle(ctx, execution.ID, entry, entity.ServiceStopped, drainingNote)

			return
		case <-time.After(waiting):
		}

		started, err := s.spawn(ctx, execution, entry)
		if err != nil {
			s.settle(ctx, execution.ID, entry, entity.ServiceUnhealthy, err.Error())

			return
		}

		listening = started
	}
}

func (s *servicesSupervisor) attend(
	ctx context.Context,
	execution entity.Execution,
	entry *supervised,
	listening *heard,
) int {
	s.mu.Lock()
	wanted := entry.wanted
	s.mu.Unlock()

	checking, done := context.WithTimeout(ctx, s.cfg.HealthTimeout)
	checked := make(chan struct{})

	go func() {
		defer close(checked)

		s.check(checking, execution.ID, entry, wanted, listening.lines)
	}()

	code, _ := listening.child.Wait()

	done()

	<-checked

	listening.forget()

	return code
}

func (s *servicesSupervisor) check(
	ctx context.Context,
	executionID string,
	entry *supervised,
	wanted entity.Service,
	lines <-chan string,
) {
	if wanted.Health.Kind == entity.HealthNone {
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.cfg.HealthInterval):
		}

		s.reach(ctx, executionID, entry, entity.ServiceHealthy, stayedUp(s.cfg.HealthInterval))

		return
	}

	s.mu.Lock()
	port := entry.record.Port
	s.mu.Unlock()

	var last error

	for ctx.Err() == nil {
		last = probe(ctx, wanted.Health, port, lines)
		if last == nil {
			s.reach(ctx, executionID, entry, entity.ServiceHealthy, answered(wanted.Health, port))

			return
		}

		select {
		case <-ctx.Done():
		case <-time.After(s.cfg.HealthInterval):
		}
	}

	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return
	}

	s.reach(ctx, executionID, entry, entity.ServiceUnhealthy, unanswered(s.cfg.HealthTimeout, last))
}

func (s *servicesSupervisor) reach(
	ctx context.Context,
	executionID string,
	entry *supervised,
	state entity.ServiceState,
	reason string,
) {
	s.mu.Lock()
	starting := entry.record.State == entity.ServiceStarting
	s.mu.Unlock()

	if !starting {
		return
	}

	s.settle(ctx, executionID, entry, state, reason)
}

func (s *servicesSupervisor) gone(entry *supervised) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry.record.PID = 0
}

func (s *servicesSupervisor) expected(entry *supervised) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return entry.stopping
}

func (s *servicesSupervisor) why(entry *supervised) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry.because == "" {
		return stoppedNote
	}

	return entry.because
}

func (s *servicesSupervisor) backoff(attempt int) time.Duration {
	waiting := s.cfg.RestartBackoff

	for range attempt - 1 {
		waiting *= 2
	}

	return waiting
}

func answered(health entity.Health, port int) string {
	switch health.Kind {
	case entity.HealthTCP:
		return fmt.Sprintf("it accepted a connection on port %d", port)
	case entity.HealthLog:
		return fmt.Sprintf("it wrote a line matching %s", health.Pattern)
	default:
		return fmt.Sprintf("it answered %s on port %d", path(health.Path), port)
	}
}

func stayedUp(within time.Duration) string {
	return fmt.Sprintf("it stayed up for %s, and nothing was asked to be checked", within)
}

func path(asked string) string {
	if asked == "" {
		return "/"
	}

	return asked
}

func unanswered(within time.Duration, last error) string {
	if last == nil {
		return fmt.Sprintf("it did not come up within %s", within)
	}

	return fmt.Sprintf("it did not come up within %s: %s", within, last)
}

func gaveUp(code int, attempts int) string {
	return fmt.Sprintf(
		"it stopped on its own with exit code %d, and %s to start it again did not hold",
		code, times(attempts),
	)
}

func retrying(name string, code int, attempt int, of int, waiting time.Duration) string {
	return fmt.Sprintf(
		"%s stopped on its own with exit code %d; starting it again in %s (attempt %d of %d)",
		name, code, waiting, attempt, of,
	)
}

func times(attempts int) string {
	if attempts == 1 {
		return "one attempt"
	}

	return fmt.Sprintf("%d attempts", attempts)
}
