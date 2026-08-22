package gitcmd_test

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/pkg/gitcmd"
)

func TestGitRunsInAGroupOfItsOwnSoTeardownTakesEverythingWithIt(t *testing.T) {
	if !gitcmd.Installed() {
		t.Skip("git is not installed, so nothing can be spawned to check")
	}

	ctx, stop := context.WithCancel(context.Background())

	command := gitcmd.Command(ctx, "", "hash-object", "--stdin")

	held, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("hold git's input open: %v", err)
	}

	if err := command.Start(); err != nil {
		t.Fatalf("start git: %v", err)
	}

	group, err := syscall.Getpgid(command.Process.Pid)
	if err != nil {
		t.Fatalf("ask which group git is in: %v", err)
	}

	if group == syscall.Getpgrp() {
		t.Fatalf(
			"git shares the runner's process group, so tearing a run down cannot kill what git " +
				"spawned without killing the runner too",
		)
	}

	if group != command.Process.Pid {
		t.Fatalf("git is in group %d rather than leading its own", group)
	}

	stop()

	settled := make(chan error, 1)

	go func() { settled <- command.Wait() }()

	select {
	case <-settled:
	case <-time.After(10 * time.Second):
		t.Fatalf("git outlived the run that started it")
	}

	_ = held.Close()
}
