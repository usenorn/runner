package control_test

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type harness struct {
	dir    *statedir.Dir
	client *control.Client
}

func settings() config.Control {
	return config.Control{
		DialTimeout:       time.Second,
		RequestTimeout:    2 * time.Second,
		ReadHeaderTimeout: time.Second,
		ShutdownTimeout:   3 * time.Second,
	}
}

func newStateDir(t *testing.T) *statedir.Dir {
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

	return dir
}

func newHarness(t *testing.T, handler http.Handler) *harness {
	t.Helper()

	dir := newStateDir(t)

	listener, cleanup, err := socket.New(dir)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	if handler == nil {
		handler = control.NewServer(
			config.Runner{Server: "https://norn.example", Capacity: 4, Runtime: config.RuntimeAuto},
			config.State{Root: dir.Root(), ConfigFile: dir.Config()},
			config.App{Version: "test"},
			dir,
		)
	}

	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}

	go func() { _ = server.Serve(listener) }()

	t.Cleanup(func() {
		_ = server.Close()
		cleanup()
	})

	return &harness{dir: dir, client: control.NewClient(settings(), dir)}
}
