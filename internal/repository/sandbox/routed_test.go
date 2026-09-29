package sandbox_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	processrepo "github.com/usenorn/runner/internal/repository/process"
	sandboxrepo "github.com/usenorn/runner/internal/repository/sandbox"
)

func TestWorkAskedToRunAsAHostProcessRunsOnTheHost(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("there is no shell here to run anything with")
	}

	var said bytes.Buffer

	code, err := sandboxrepo.New(processrepo.New()).Run(
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
	sandboxes := sandboxrepo.New(processrepo.New())

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
