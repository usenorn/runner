package socket

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

func aListener(t *testing.T) *Listener {
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

	listener, cleanup, err := New(dir)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(cleanup)

	return listener
}

func ask(t *testing.T, listener *Listener) error {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}

	go func() { _ = server.Serve(listener) }()

	t.Cleanup(func() { _ = server.Close() })

	path := listener.Path()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", path)
			},
		},
		Timeout: 2 * time.Second,
	}

	request, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "http://runner/ping", nil,
	)
	if err != nil {
		t.Fatalf("build a request: %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return err
	}

	defer func() { _ = response.Body.Close() }()

	return nil
}

func TestTheSocketAnswersTheAccountThatOwnsIt(t *testing.T) {
	listener := aListener(t)

	if listener.owner != os.Getuid() {
		t.Fatalf("the listener guards uid %d, want this account %d", listener.owner, os.Getuid())
	}

	if err := ask(t, listener); err != nil {
		t.Fatalf("the owner was refused by its own runner: %v", err)
	}
}

func TestTheSocketRefusesAnAccountThatDoesNotOwnIt(t *testing.T) {
	listener := aListener(t)
	listener.owner = os.Getuid() + 1

	if err := ask(t, listener); err == nil {
		t.Fatalf(
			"a connection from another account was served; file permissions must not be the only guard",
		)
	}
}
