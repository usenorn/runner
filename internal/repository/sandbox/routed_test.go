package sandbox_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/bridge"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	portrepo "github.com/usenorn/runner/internal/repository/port"
	processrepo "github.com/usenorn/runner/internal/repository/process"
	sandboxrepo "github.com/usenorn/runner/internal/repository/sandbox"
)

func TestWorkAskedToRunAsAHostProcessRunsOnTheHost(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("there is no shell here to run anything with")
	}

	var said bytes.Buffer

	code, err := hostOnly(t).Run(
		context.Background(),
		entity.Sandbox{Run: "exec-01ABC", Runtime: entity.RuntimeProcess},
		repository.Launch{Command: []string{"sh", "-c", "echo on the host"}, Output: &said},
		time.Minute,
	)
	if err != nil || code != 0 {
		t.Fatalf("a host process came back %d, %v", code, err)
	}

	if !strings.Contains(said.String(), "on the host") {
		t.Fatalf("the host process said %q", said.String())
	}
}

func TestARuntimeThisMachineCannotRunWorkInIsRefusedByName(t *testing.T) {
	sandboxes := hostOnly(t)

	err := sandboxes.Check(context.Background(), entity.Runtime("kvm"))
	if !errors.Is(err, entity.ErrRuntimeUnsupported) || !strings.Contains(err.Error(), "kvm") {
		t.Fatalf(
			"asking for kvm came back %v. A run that asked for a runtime nothing here can "+
				"start must be turned down saying so, never run on the host instead",
			err,
		)
	}

	if _, err := sandboxes.Start(
		context.Background(), entity.Sandbox{Runtime: "kvm"}, repository.Launch{Command: []string{"true"}},
	); !errors.Is(err, entity.ErrRuntimeUnsupported) {
		t.Fatalf("starting work in kvm came back %v, want it refused", err)
	}
}

func hostOnly(t *testing.T) repository.Sandbox {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	bridged, closeBridge, err := bridge.New(config.Docker{Bridge: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("listen for containers: %v", err)
	}

	t.Cleanup(closeBridge)

	return sandboxrepo.New(
		processrepo.New(),
		portrepo.New(config.Runner{PortRange: [2]int{46100, 46199}}),
		dir,
		config.Docker{Image: "ghcr.io/usenorn/runner-sandbox:test", Ports: 1, Timeout: time.Second, PullTimeout: time.Second},
		bridged,
	)
}
