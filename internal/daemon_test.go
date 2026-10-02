package internal_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal"
	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/mcpbridge"
	"github.com/usenorn/runner/internal/pkg/bridge"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/pkg/statedir"
	channelsvc "github.com/usenorn/runner/internal/service/channel"
	codebasesvc "github.com/usenorn/runner/internal/service/codebase"
	executionsvc "github.com/usenorn/runner/internal/service/execution"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
	supervisorsvc "github.com/usenorn/runner/internal/service/supervisor"
	tunnelsvc "github.com/usenorn/runner/internal/service/tunnel"
	updatesvc "github.com/usenorn/runner/internal/service/update"
	uploadsvc "github.com/usenorn/runner/internal/service/upload"
)

type startup struct {
	mu    sync.Mutex
	order []string
}

func (s *startup) saw(what string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.order = append(s.order, what)
}

func (s *startup) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.order)
}

func newDaemon(t *testing.T, shutdown time.Duration, handler http.Handler) (*internal.Daemon, *statedir.Dir) {
	t.Helper()

	daemon, dir, _ := newDaemonRecording(t, shutdown, handler)

	return daemon, dir
}

func newDaemonRecording(
	t *testing.T,
	shutdown time.Duration,
	handler http.Handler,
) (*internal.Daemon, *statedir.Dir, *startup) {
	t.Helper()

	started := &startup{}

	root, err := os.MkdirTemp("/tmp", "nrn")
	if err != nil {
		t.Fatalf("create temporary root: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	listener, cleanup, err := socket.New(dir)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(cleanup)

	cfg := config.Control{
		DialTimeout:       time.Second,
		RequestTimeout:    time.Second,
		ReadHeaderTimeout: time.Second,
		ShutdownTimeout:   shutdown,
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	ctrl := gomock.NewController(t)

	sessions := sessionsvc.NewMockSessions(ctrl)
	sessions.EXPECT().Run(gomock.Any()).AnyTimes()

	updates := updatesvc.NewMockUpdates(ctrl)
	updates.EXPECT().Run(gomock.Any()).AnyTimes()

	codebases := codebasesvc.NewMockCodebases(ctrl)
	codebases.EXPECT().Run(gomock.Any()).AnyTimes()

	channels := channelsvc.NewMockChannels(ctrl)
	channels.EXPECT().
		Run(gomock.Any()).
		Do(func(context.Context) { started.saw("channel") }).
		AnyTimes()

	tunnels := tunnelsvc.NewMockTunnels(ctrl)
	tunnels.EXPECT().Run(gomock.Any()).AnyTimes()

	runs := executionsvc.NewMockExecutions(ctrl)
	runs.EXPECT().
		Reclaim(gomock.Any()).
		DoAndReturn(func(context.Context) error {
			time.Sleep(20 * time.Millisecond)
			started.saw("reclaim")

			return nil
		}).
		AnyTimes()
	runs.EXPECT().Run(gomock.Any()).AnyTimes()

	services := supervisorsvc.NewMockServices(ctrl)
	services.EXPECT().Run(gomock.Any()).AnyTimes()

	uploads := uploadsvc.NewMockUploads(ctrl)

	bridged, closeBridge, err := bridge.New(config.Docker{Bridge: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("listen for containers: %v", err)
	}

	t.Cleanup(closeBridge)

	tools, closeTools := mcpbridge.New(cfg, config.Questions{}, config.Supervisor{}, dir, config.App{Version: "test"})
	t.Cleanup(closeTools)

	return internal.NewDaemon(
		cfg, handler, listener, bridged, tools, sessions, updates, codebases, channels, tunnels,
		runs, services, uploads, logger,
	), dir, started
}

func TestTheRunsAMachineWasHoldingAreReadBackBeforeItTalksToNorn(t *testing.T) {
	daemon, _, started := newDaemonRecording(t, 2*time.Second, http.NewServeMux())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() { done <- daemon.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	order := started.seen()
	if !slices.Equal(order, []string{"reclaim", "channel"}) {
		t.Fatalf(
			"the machine started in the order %v. Norn's first message lists the runs it "+
				"believes this machine holds, and one heard before the machine has read its own "+
				"back is taken for a run it lost and failed",
			order,
		)
	}
}

func TestCancellingTheContextDrainsAndReturnsNothing(t *testing.T) {
	daemon, _ := newDaemon(t, 2*time.Second, http.NewServeMux())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() { done <- daemon.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a clean drain returned %v, want nothing", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the daemon never returned after its context was cancelled")
	}
}

func TestARequestStillRunningPastTheDrainDeadlineForcesTheExitCode(t *testing.T) {
	held := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+control.StatusPath, func(http.ResponseWriter, *http.Request) {
		<-held
	})

	daemon, dir := newDaemon(t, 100*time.Millisecond, mux)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() { done <- daemon.Run(ctx) }()

	client := control.NewClient(
		config.Control{DialTimeout: time.Second, RequestTimeout: 5 * time.Second},
		config.Questions{SoftWait: 20 * time.Millisecond, MaxWait: time.Second},
		config.Supervisor{StepTimeout: time.Minute},
		dir,
		"",
	)

	asked := make(chan struct{})

	go func() {
		defer close(asked)

		_, _ = client.Status(context.Background())
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if code := entity.ExitCode(err); code != entity.ExitDrainForced {
			t.Fatalf("a forced drain exited %d, want %d so an operator can tell it apart from a clean stop",
				code, entity.ExitDrainForced)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the daemon hung instead of forcing the drain")
	}

	close(held)
	<-asked
}
