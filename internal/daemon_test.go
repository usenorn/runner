package internal_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal"
	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/pkg/statedir"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
)

func newDaemon(t *testing.T, shutdown time.Duration, handler http.Handler) (*internal.Daemon, *statedir.Dir) {
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

	sessions := sessionsvc.NewMockSessions(gomock.NewController(t))
	sessions.EXPECT().Run(gomock.Any()).AnyTimes()

	return internal.NewDaemon(cfg, handler, listener, sessions, logger), dir
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
		config.Control{DialTimeout: time.Second, RequestTimeout: 5 * time.Second}, dir,
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
