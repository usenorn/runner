package bridge_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/bridge"
)

func TestAContainerIsToldToReachTheRunnerThroughTheHostsOwnName(t *testing.T) {
	listener, cleanup, err := bridge.New(config.Docker{Bridge: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(cleanup)

	reach, err := listener.Reach()
	if err != nil {
		t.Fatalf("reach: %v", err)
	}

	if !strings.HasPrefix(reach, "http://host.docker.internal:") || strings.HasSuffix(reach, ":0") {
		t.Fatalf("a container is told to reach the runner at %q", reach)
	}
}

func TestAMachineWhereContainersCannotReachTheRunnerStillStarts(t *testing.T) {
	listener, cleanup, err := bridge.New(config.Docker{Bridge: "203.0.113.1:0"})
	if err != nil {
		t.Fatalf(
			"a bridge that cannot be opened stopped the runner starting: %v. Only runs in "+
				"docker need it, and they are turned down instead",
			err,
		)
	}

	t.Cleanup(cleanup)

	if _, err := listener.Reach(); !errors.Is(err, bridge.ErrUnavailable) {
		t.Fatalf("reaching an unopened bridge came back %v, want it said to be unavailable", err)
	}
}
