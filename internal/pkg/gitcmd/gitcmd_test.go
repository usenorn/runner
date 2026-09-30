package gitcmd_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestARepositoryCannotMakeTheRunnersGitRunItsCode(t *testing.T) {
	if !gitcmd.Installed() {
		t.Skip("git is not installed, so nothing can be spawned to check")
	}

	ctx := context.Background()
	repository := t.TempDir()
	planted := filepath.Join(t.TempDir(), "ran")

	if _, err := gitcmd.Run(ctx, repository, "init", "--quiet"); err != nil {
		t.Fatalf("init: %v", err)
	}

	hook := "#!/bin/sh\ntouch " + planted + "\n"
	for _, name := range []string{"pre-commit", "post-commit", "reference-transaction"} {
		if err := os.WriteFile(filepath.Join(repository, ".git", "hooks", name), []byte(hook), 0o755); err != nil {
			t.Fatalf("plant %s: %v", name, err)
		}
	}

	if _, err := gitcmd.Run(
		ctx, repository, "config", "core.fsmonitor", filepath.Join(repository, ".git", "hooks", "pre-commit"),
	); err != nil {
		t.Fatalf("plant fsmonitor: %v", err)
	}

	if _, err := gitcmd.Run(ctx, repository,
		"-c", "user.name=Norn", "-c", "user.email=runner@norn.invalid",
		"commit", "--quiet", "--allow-empty", "--no-gpg-sign", "-m", "empty",
	); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if _, err := gitcmd.Run(ctx, repository, "status", "--porcelain"); err != nil {
		t.Fatalf("status: %v", err)
	}

	if _, err := os.Stat(planted); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(
			"a hook the repository planted ran under the runner's own git (%v); the runner "+
				"holds the person's credentials, so anything the agent writes into .git runs with them",
			err,
		)
	}
}
