package execution_test

import (
	"context"
	"sync"
	"time"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

type script struct {
	session string
	events  []entity.DriverEvent
	logs    []string
	result  entity.DriverResult
	err     error
	hold    chan struct{}
}

func finishes(session string, summary string) script {
	return script{
		session: session,
		result: entity.DriverResult{
			Outcome: entity.OutcomeDone,
			Summary: summary,
			Usage:   entity.DriverUsage{Turns: 2, Took: time.Second},
		},
	}
}

func crashes(session string) script {
	return script{
		session: session,
		result:  entity.DriverResult{Outcome: entity.OutcomeCrashed, ExitCode: 137},
	}
}

func holds(session string) script {
	held := finishes(session, "held open by the test")
	held.hold = make(chan struct{})

	return held
}

type driverStub struct {
	mu sync.Mutex

	health   entity.DriverHealth
	scripts  []script
	starts   []entity.ExecEnv
	tasks    []entity.Task
	resumed  []entity.DriverSession
	injected []string
	stopped  int
}

func newDriverStub() *driverStub {
	return &driverStub{
		scripts: []script{finishes("session-01", "the work is committed")},
		health: entity.DriverHealth{
			Kind:      entity.DriverClaude,
			Installed: true,
			Version:   "2.1.239",
			SignedIn:  true,
			Account:   "runner@example",
		},
	}
}

func (d *driverStub) Preflight(context.Context, entity.DriverKind) entity.DriverHealth {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.health
}

func (d *driverStub) Start(
	_ context.Context,
	env entity.ExecEnv,
	task entity.Task,
) (repository.Session, error) {
	d.mu.Lock()
	d.starts = append(d.starts, env)
	d.tasks = append(d.tasks, task)
	d.mu.Unlock()

	return d.play()
}

func (d *driverStub) Resume(
	_ context.Context,
	_ entity.ExecEnv,
	held entity.DriverSession,
	injection string,
) (repository.Session, error) {
	d.mu.Lock()
	d.resumed = append(d.resumed, held)
	d.injected = append(d.injected, injection)
	d.mu.Unlock()

	return d.play()
}

func (d *driverStub) play() (repository.Session, error) {
	d.mu.Lock()

	if len(d.scripts) == 0 {
		d.mu.Unlock()

		return nil, entity.ErrDriverMissing
	}

	next := d.scripts[0]
	d.scripts = d.scripts[1:]

	d.mu.Unlock()

	if next.err != nil {
		return nil, next.err
	}

	return newSessionStub(d, next), nil
}

func (d *driverStub) began() []entity.Task {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]entity.Task(nil), d.tasks...)
}

func (d *driverStub) carried() []entity.DriverSession {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]entity.DriverSession(nil), d.resumed...)
}

func (d *driverStub) worked() []entity.ExecEnv {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]entity.ExecEnv(nil), d.starts...)
}

type sessionStub struct {
	driver  *driverStub
	events  chan entity.DriverEvent
	logs    chan string
	hold    chan struct{}
	letting sync.Once
	held    entity.DriverSession
	result  entity.DriverResult
}

func newSessionStub(driver *driverStub, played script) *sessionStub {
	held := &sessionStub{
		driver: driver,
		events: make(chan entity.DriverEvent, len(played.events)+1),
		logs:   make(chan string, len(played.logs)+1),
		hold:   played.hold,
		held: entity.DriverSession{
			ID:        played.session,
			StartedAt: time.Now().UTC(),
			Outcome:   played.result.Outcome,
			Reason:    played.result.Summary,
		},
		result: played.result,
	}

	go func() {
		for _, event := range played.events {
			held.events <- event
		}

		for _, line := range played.logs {
			held.logs <- line
		}

		if played.hold != nil {
			<-played.hold
		}

		close(held.events)
		close(held.logs)
	}()

	return held
}

func (s *sessionStub) Events() <-chan entity.DriverEvent {
	return s.events
}

func (s *sessionStub) Logs() <-chan string {
	return s.logs
}

func (s *sessionStub) Reference() entity.DriverSession {
	return s.held
}

func (s *sessionStub) Wait() (entity.DriverResult, error) {
	return s.result, nil
}

func (s *sessionStub) Stop(context.Context, time.Duration) error {
	if s.hold != nil {
		s.letting.Do(func() { close(s.hold) })
	}

	s.driver.mu.Lock()
	defer s.driver.mu.Unlock()

	s.driver.stopped++

	return nil
}
