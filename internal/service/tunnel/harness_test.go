package tunnel_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
	"os"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	"github.com/usenorn/runner/internal/service"
	previewsvc "github.com/usenorn/runner/internal/service/preview"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
	tunnelsvc "github.com/usenorn/runner/internal/service/tunnel"
)

const (
	settle        = 2 * time.Second
	testExecution = "exec-01ABCDEF"
)

func settings() config.Tunnel {
	return config.Tunnel{
		Enabled:          true,
		HandshakeTimeout: settle,
		Heartbeat:        time.Second,
		DialTimeout:      settle,
		WriteTimeout:     settle,
		RetryMin:         10 * time.Millisecond,
		RetryMax:         100 * time.Millisecond,
		MaxStreams:       4,
	}
}

type gateway struct {
	mu       sync.Mutex
	attached chan struct{}
	session  *carrier
	dials    int
	failing  error
}

func (g *gateway) Dial(
	_ context.Context,
	_ entity.TunnelTicket,
) (repository.TunnelSession, error) {
	g.mu.Lock()
	g.dials++
	failing := g.failing
	g.mu.Unlock()

	if failing != nil {
		return nil, failing
	}

	session := &carrier{opened: make(chan net.Conn, 8), closed: make(chan struct{})}

	g.mu.Lock()
	g.session = session
	g.mu.Unlock()

	select {
	case g.attached <- struct{}{}:
	default:
	}

	return session, nil
}

type carrier struct {
	opened chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *carrier) Accept() (net.Conn, error) {
	select {
	case stream := <-c.opened:
		return stream, nil
	case <-c.closed:
		return nil, errors.New("the tunnel closed")
	}
}

func (c *carrier) Close() error {
	c.once.Do(func() { close(c.closed) })

	return nil
}

func (c *carrier) open(t *testing.T, ask channelv1.StreamOpen) *channelv1.Stream {
	t.Helper()

	here, there := net.Pipe()

	select {
	case c.opened <- there:
	case <-time.After(settle):
		t.Fatal("the machine never accepted a stream from the gateway")
	}

	stream := channelv1.NewStream(here)

	if err := stream.WriteFrame(ask); err != nil {
		t.Fatalf("open a stream: %v", err)
	}

	return stream
}

func (c *carrier) ready(t *testing.T, stream *channelv1.Stream) channelv1.StreamReady {
	t.Helper()

	if err := stream.SetReadDeadline(time.Now().Add(settle)); err != nil {
		t.Fatalf("set a deadline: %v", err)
	}

	var answered channelv1.StreamReady

	if err := stream.ReadFrame(&answered); err != nil {
		t.Fatalf("read what the machine answered: %v", err)
	}

	_ = stream.SetReadDeadline(time.Time{})

	return answered
}

type harness struct {
	t        *testing.T
	gateway  *gateway
	previews service.Previews
	tunnels  service.Tunnels
	runs     repository.Run
}

func newHarness(t *testing.T, cfg config.Tunnel) *harness {
	t.Helper()

	return build(t, cfg, nil)
}

func newRefusedHarness(t *testing.T, cfg config.Tunnel, refusing error) *harness {
	t.Helper()

	return build(t, cfg, refusing)
}

func build(t *testing.T, cfg config.Tunnel, refusing error) *harness {
	t.Helper()

	ctrl := gomock.NewController(t)

	sessions := sessionsvc.NewMockSessions(ctrl)
	sessions.EXPECT().
		TunnelTicket(gomock.Any()).
		Return(entity.TunnelTicket{
			Ticket:   "nru_ticket",
			Previews: entity.PreviewService{Gateway: "https://tunnel.norn.ink", Domain: "norn.ink"},
		}, nil).
		AnyTimes()

	sessions.EXPECT().
		Previews().
		Return(entity.PreviewService{Gateway: "https://tunnel.norn.ink", Domain: "norn.ink"}).
		AnyTimes()

	runs, spool := store(t)
	previews := previewsvc.New(runs, spool, sessions)

	held := &gateway{attached: make(chan struct{}, 1), failing: refusing}
	tunnels := tunnelsvc.New(held, sessions, previews, cfg)

	ctx, stop := context.WithCancel(context.Background())

	go tunnels.Run(ctx)

	t.Cleanup(stop)

	return &harness{t: t, gateway: held, previews: previews, tunnels: tunnels, runs: runs}
}

func (h *harness) dialled() int {
	h.gateway.mu.Lock()
	defer h.gateway.mu.Unlock()

	return h.gateway.dials
}

func store(t *testing.T) (repository.Run, repository.Spool) {
	t.Helper()

	root, err := os.MkdirTemp("/tmp", "nrn")
	if err != nil {
		t.Fatalf("create temporary root: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	return runrepo.New(dir), spoolrepo.New(dir)
}

func (h *harness) running(name string, port int, state entity.ServiceState) {
	h.t.Helper()

	ctx := context.Background()

	if _, err := h.runs.Open(ctx, testExecution); err != nil {
		h.t.Fatalf("make a run directory: %v", err)
	}

	if err := h.runs.SaveTask(ctx, entity.Execution{
		ID:         testExecution,
		Reference:  "NORN-53",
		IssueKey:   "NORN-53",
		Attempt:    1,
		Title:      "Preview proxy and tunnel client",
		State:      channelv1.StateRunning,
		AcceptedAt: time.Now().UTC(),
	}); err != nil {
		h.t.Fatalf("write a task: %v", err)
	}

	record := entity.ServiceRecord{
		Name:      name,
		Command:   []string{"pnpm", "dev"},
		Port:      port,
		PID:       4242,
		State:     state,
		StartedAt: time.Now().UTC(),
	}

	if err := h.runs.SaveServices(ctx, testExecution, entity.RunServices{
		Runtime:  "process",
		Ports:    map[string]int{name: port},
		Services: []entity.ServiceRecord{record},
	}); err != nil {
		h.t.Fatalf("write the services: %v", err)
	}
}

func (h *harness) exposed(name string) {
	h.t.Helper()

	if _, err := h.previews.Expose(
		context.Background(), testExecution, entity.Preview{Name: name, Service: name},
	); err != nil {
		h.t.Fatalf("expose %s: %v", name, err)
	}
}

func (h *harness) attached() *carrier {
	h.t.Helper()

	select {
	case <-h.gateway.attached:
	case <-time.After(settle):
		h.t.Fatal("this machine never opened a tunnel to the gateway")
	}

	h.gateway.mu.Lock()
	defer h.gateway.mu.Unlock()

	return h.gateway.session
}
