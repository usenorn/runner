package control_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
)

func TestStatusAnswersOverTheSocketWithWhatTheRunnerIs(t *testing.T) {
	h := newHarness(t, nil)

	status, err := h.client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status.PID != os.Getpid() {
		t.Fatalf("status reported pid %d, want this process %d", status.PID, os.Getpid())
	}

	if status.Socket != h.dir.Socket() {
		t.Fatalf("status reported socket %q, want %q", status.Socket, h.dir.Socket())
	}

	if status.Capacity != 4 || status.Runtime != string(config.RuntimeAuto) {
		t.Fatalf("status did not carry the configuration: %+v", status)
	}

	if status.StartedAt.IsZero() {
		t.Fatalf("status reported no start time")
	}
}

func TestAFreshRunnerReportsItselfUnenrolledUntilAnIdentityExists(t *testing.T) {
	h := newHarness(t, nil)

	status, err := h.client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status.Enrolled {
		t.Fatalf("a runner with no identity file reported itself enrolled")
	}
}

func TestAskingAnUnknownPathIsRefused(t *testing.T) {
	h := newHarness(t, http.NewServeMux())

	if _, err := h.client.Status(context.Background()); err == nil {
		t.Fatalf("an unrouted path answered as though it were status")
	}
}

func TestStatusWithNoRunnerListeningFailsAtOnceAndSaysHowToStartOne(t *testing.T) {
	dir := newStateDir(t)
	client := control.NewClient(settings(), dir)

	started := time.Now()

	_, err := client.Status(context.Background())
	if err == nil {
		t.Fatalf("status answered with no daemon running")
	}

	if code := entity.ExitCode(err); code != entity.ExitDaemonUnavailable {
		t.Fatalf("exit code is %d, want %d so a script can tell this apart from a real failure",
			code, entity.ExitDaemonUnavailable)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("status took %s to notice nothing was listening; it must not hang", elapsed)
	}
}

func TestARunnerThatAcceptsButNeverAnswersIsGivenUpOn(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+control.StatusPath, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	h := newHarness(t, mux)

	started := time.Now()

	if _, err := h.client.Status(context.Background()); err == nil {
		t.Fatalf("a daemon that never answered was treated as success")
	}

	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("giving up took %s, longer than the request timeout allows", elapsed)
	}
}
