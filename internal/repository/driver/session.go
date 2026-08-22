package driver

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

const buffered = 256

type lines struct {
	limit int
	take  func([]byte)

	pending []byte
	dropped bool
}

func (l *lines) Write(raw []byte) (int, error) {
	l.pending = append(l.pending, raw...)

	for {
		at := bytes.IndexByte(l.pending, '\n')
		if at < 0 {
			break
		}

		l.emit(bytes.TrimRight(l.pending[:at], "\r"))

		l.pending = l.pending[at+1:]
	}

	// A line longer than the cap is dropped rather than buffered: the coding agent can put a whole
	// file on one line, and holding it whole would let a single tool result take the daemon's memory.
	if len(l.pending) > l.limit {
		l.pending = l.pending[:0]
		l.dropped = true
	}

	return len(raw), nil
}

func (l *lines) emit(line []byte) {
	over := l.dropped || len(line) > l.limit
	l.dropped = false

	if over || len(line) == 0 {
		return
	}

	l.take(line)
}

func (l *lines) close() {
	if len(l.pending) > 0 {
		l.emit(l.pending)

		l.pending = nil
	}
}

type claudeSession struct {
	child repository.Child
	now   func() time.Time

	events  chan entity.DriverEvent
	logs    chan string
	closed  chan struct{}
	over    chan struct{}
	letting sync.Once
	ending  sync.Once

	mu      sync.Mutex
	held    entity.DriverSession
	told    *entity.DriverResult
	reading *reading

	outcome entity.DriverResult
	err     error
}

func newSession(held entity.DriverSession, now func() time.Time) *claudeSession {
	return &claudeSession{
		now:     now,
		events:  make(chan entity.DriverEvent, buffered),
		logs:    make(chan string, buffered),
		closed:  make(chan struct{}),
		over:    make(chan struct{}),
		held:    held,
		reading: newReading(),
	}
}

func (s *claudeSession) Events() <-chan entity.DriverEvent {
	return s.events
}

func (s *claudeSession) Logs() <-chan string {
	return s.logs
}

func (s *claudeSession) Reference() entity.DriverSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.held
}

func (s *claudeSession) Wait() (entity.DriverResult, error) {
	<-s.over

	return s.outcome, s.err
}

func (s *claudeSession) Stop(ctx context.Context, grace time.Duration) error {
	s.abandon()

	if s.child == nil {
		return nil
	}

	return s.child.Stop(ctx, grace)
}

// abandon unblocks anything still handing a line over, so a caller that has stopped reading does
// not leave the child's own write blocked and its Wait unable to return.
func (s *claudeSession) abandon() {
	s.letting.Do(func() { close(s.closed) })
}

func (s *claudeSession) spoken() *lines {
	return &lines{limit: entity.DriverLineMax, take: s.spoke}
}

func (s *claudeSession) complained() *lines {
	return &lines{limit: entity.DriverLineMax, take: func(line []byte) {
		s.log(string(line))
	}}
}

func (s *claudeSession) spoke(line []byte) {
	s.mu.Lock()

	if s.held.ID == "" {
		if found := s.reading.session(line); found != "" {
			s.held.ID = found
			s.held.StartedAt = s.now()
		}
	}

	events, told, err := s.reading.read(line, s.now())

	if told != nil {
		s.told = told
	}

	s.mu.Unlock()

	if err != nil {
		s.log(string(line))

		return
	}

	for _, event := range events {
		s.send(event)
	}
}

func (s *claudeSession) send(event entity.DriverEvent) {
	select {
	case s.events <- event:
	case <-s.closed:
	}
}

func (s *claudeSession) log(line string) {
	select {
	case s.logs <- line:
	case <-s.closed:
	}
}

func (s *claudeSession) settle(code int, err error) {
	s.mu.Lock()

	told := s.told
	s.held.EndedAt = s.now()

	s.mu.Unlock()

	switch {
	case told != nil:
		s.outcome = *told
	default:
		s.outcome = entity.DriverResult{Outcome: entity.OutcomeCrashed}
	}

	s.outcome.ExitCode = code
	s.err = err

	s.mu.Lock()
	s.held.Outcome = s.outcome.Outcome
	s.held.Reason = s.outcome.Summary
	s.mu.Unlock()

	s.ending.Do(func() {
		s.abandon()

		close(s.events)
		close(s.logs)
		close(s.over)
	})
}
