package channel_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	channelrepo "github.com/usenorn/runner/internal/repository/channel"
	runtokenrepo "github.com/usenorn/runner/internal/repository/runtoken"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	"github.com/usenorn/runner/internal/service"
	channelsvc "github.com/usenorn/runner/internal/service/channel"
	executionsvc "github.com/usenorn/runner/internal/service/execution"
	previewsvc "github.com/usenorn/runner/internal/service/preview"
	questionsvc "github.com/usenorn/runner/internal/service/question"
)

type wire struct {
	mu sync.Mutex

	inbound  chan channelv1.Envelope
	outbound chan channelv1.Envelope
	closed   chan struct{}
	ended    error
	autoAck  bool
}

func newWire(autoAck bool) *wire {
	return &wire{
		inbound:  make(chan channelv1.Envelope, 64),
		outbound: make(chan channelv1.Envelope, 64),
		closed:   make(chan struct{}),
		autoAck:  autoAck,
	}
}

func (w *wire) Read(ctx context.Context) (channelv1.Envelope, error) {
	select {
	case envelope := <-w.inbound:
		return envelope, nil
	case <-w.closed:
		return channelv1.Envelope{}, w.reason()
	case <-ctx.Done():
		return channelv1.Envelope{}, ctx.Err()
	}
}

func (w *wire) Write(ctx context.Context, envelope channelv1.Envelope) error {
	select {
	case <-w.closed:
		return w.reason()
	default:
	}

	select {
	case w.outbound <- envelope:
	case <-ctx.Done():
		return ctx.Err()
	}

	if w.autoAck && !envelope.Acknowledging() {
		select {
		case w.inbound <- channelv1.Acknowledgement(envelope.ID, time.Now().UTC()):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (w *wire) Close() error {
	w.hangUp(nil)

	return nil
}

func (w *wire) hangUp(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.closed:
		return
	default:
	}

	if err == nil {
		err = errors.New("the wire was closed")
	}

	w.ended = err

	close(w.closed)
}

func (w *wire) reason() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.ended
}

func (w *wire) send(t *testing.T, kind channelv1.MessageType, executionID string, payload []byte) {
	t.Helper()

	message, err := channelv1.NewServerMessage(kind, executionID, payload, time.Now().UTC())
	if err != nil {
		t.Fatalf("build a %s: %v", kind, err)
	}

	w.inbound <- channelv1.Frame(message)
}

func (w *wire) await(t *testing.T, kind channelv1.MessageType) channelv1.Envelope {
	t.Helper()

	deadline := time.After(5 * time.Second)

	for {
		select {
		case envelope := <-w.outbound:
			if channelv1.MessageType(envelope.Type) == kind {
				return envelope
			}
		case <-deadline:
			t.Fatalf("the machine never sent a %s", kind)
		}
	}
}

type harness struct {
	mu sync.Mutex

	dir        *statedir.Dir
	spool      repository.Spool
	dials      *channelrepo.MockChannel
	sessions   *sessionMock
	executions service.Executions
	questions  service.Questions
	service    service.Channels
	wires      []*wire
	dialled    chan *wire
}

type sessionMock struct {
	service.Sessions

	mu      sync.Mutex
	ticket  string
	err     error
	handout int
}

func (s *sessionMock) Ticket(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.handout++

	if s.err != nil {
		return "", s.err
	}

	return s.ticket, nil
}

func (s *sessionMock) handouts() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.handout
}

func newHarness(t *testing.T, autoAck bool, wires int) *harness {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	ctrl := gomock.NewController(t)

	h := &harness{
		dir:      dir,
		spool:    spoolrepo.New(dir),
		dials:    channelrepo.NewMockChannel(ctrl),
		sessions: &sessionMock{ticket: "nrt_good"},
		dialled:  make(chan *wire, 16),
	}

	for range wires {
		h.wires = append(h.wires, newWire(autoAck))
	}

	h.questions = questionsvc.New(
		runStub{}, h.spool, config.Questions{SoftWait: time.Millisecond, MaxWait: time.Second},
	)

	h.executions = executionsvc.New(
		runStub{},
		h.spool,
		diskStub{},
		settingsStub{},
		inventoryStub{},
		snapshotStub{},
		servicesStub{},
		uploadStub{},
		h.questions,
		previewsvc.New(runStub{}, h.spool),
		changesetStub{},
		runtokenrepo.New(),
		driverStub{},
		dir,
		config.Runner{Capacity: 2},
		config.App{Version: "1.4.0"},
		config.Scheduler{},
		config.Driver{
			Profile:        config.ProfileStandard,
			ProbeTimeout:   time.Second,
			SessionTimeout: time.Minute,
			StopGrace:      time.Second,
		},
	)

	h.dials.EXPECT().
		Dial(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, ticket, version string) (repository.Conn, error) {
			h.mu.Lock()
			defer h.mu.Unlock()

			if len(h.wires) == 0 {
				return nil, errors.New("no more connections")
			}

			held := h.wires[0]
			h.wires = h.wires[1:]

			h.dialled <- held

			return held, nil
		}).
		AnyTimes()

	h.service = channelsvc.New(
		h.dials,
		h.spool,
		h.sessions,
		h.executions,
		h.questions,
		settings(),
		config.Spool{MaxMessages: 100, MaxAge: time.Hour, Batch: 8},
		config.App{Version: "1.4.0"},
	)

	return h
}

func offChannel(t *testing.T, h *harness) service.Channels {
	t.Helper()

	off := settings()
	off.Enabled = false

	return channelsvc.New(
		h.dials,
		h.spool,
		h.sessions,
		h.executions,
		h.questions,
		off,
		config.Spool{MaxMessages: 100, MaxAge: time.Hour, Batch: 8},
		config.App{Version: "1.4.0"},
	)
}

func settings() config.Channel {
	return config.Channel{
		Enabled:          true,
		HandshakeTimeout: time.Second,
		Heartbeat:        50 * time.Millisecond,
		WriteTimeout:     time.Second,
		RetryMin:         10 * time.Millisecond,
		RetryMax:         50 * time.Millisecond,
		MaxMessageBytes:  1 << 20,
	}
}

func (h *harness) start(t *testing.T) func() {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		h.service.Run(ctx)
	}()

	return func() {
		cancel()

		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatalf("the channel loop did not stop when its context was cancelled")
		}
	}
}

func (h *harness) awaitEmptySpool(t *testing.T) {
	t.Helper()

	deadline := time.After(5 * time.Second)

	for {
		waiting, err := h.spool.Count(context.Background())
		if err != nil {
			t.Fatalf("count the spool: %v", err)
		}

		if waiting == 0 {
			return
		}

		select {
		case <-deadline:
			t.Fatalf("%d events are still waiting after norn answered every one", waiting)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (h *harness) awaitDial(t *testing.T) *wire {
	t.Helper()

	select {
	case held := <-h.dialled:
		return held
	case <-time.After(5 * time.Second):
		t.Fatalf("the machine never opened a channel")
	}

	return nil
}

func (h *harness) queue(t *testing.T, count int) []string {
	t.Helper()

	return h.queueAged(t, count, 0)
}

func (h *harness) queueAged(t *testing.T, count int, age time.Duration) []string {
	t.Helper()

	written := make([]string, 0, count)

	for range count {
		message, err := channelv1.NewRunnerMessage(
			channelv1.ExecutionEvent,
			"exec-01ABC",
			[]byte(`{"kind":"note"}`),
			time.Now().UTC().Add(-age),
		)
		if err != nil {
			t.Fatalf("build an event: %v", err)
		}

		if err := h.spool.Append(context.Background(), message); err != nil {
			t.Fatalf("append: %v", err)
		}

		written = append(written, message.ID)
	}

	return written
}

type changesetStub struct{}

func (changesetStub) Uncommitted(
	context.Context, entity.Snapshot,
) ([]entity.UncommittedWork, error) {
	return nil, nil
}

func (changesetStub) Publish(
	context.Context, entity.Execution, entity.Snapshot, entity.Completion,
) (entity.ChangeSet, error) {
	return entity.ChangeSet{}, nil
}
