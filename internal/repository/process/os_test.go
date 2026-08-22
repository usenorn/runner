package process_test

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/repository"
	processrepo "github.com/usenorn/runner/internal/repository/process"
)

const patience = 10 * time.Second

type recorder struct {
	mu   sync.Mutex
	held bytes.Buffer
}

func (r *recorder) Write(raw []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.held.Write(raw)
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.held.String()
}

func shell(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("there is no shell here, so nothing can be spawned to check")
	}
}

func TestStoppingAServiceTakesEverythingItStartedWithIt(t *testing.T) {
	shell(t)

	processes := processrepo.New()
	ctx := context.Background()

	held := &recorder{}

	child, err := processes.Start(ctx, repository.Launch{
		Command: []string{"sh", "-c", "sleep 300 & echo up; sleep 300"},
		Output:  held,
	})
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	group, err := syscall.Getpgid(child.PID())
	if err != nil {
		t.Fatalf("ask which group the service is in: %v", err)
	}

	if group == syscall.Getpgrp() {
		t.Fatalf(
			"the service shares the runner's process group, so stopping it cannot kill what it " +
				"started without killing the runner too",
		)
	}

	until(t, "waited for the service to write something", func() bool {
		return strings.Contains(held.String(), "up")
	})

	if err := child.Stop(ctx, time.Second); err != nil {
		t.Fatalf("stop the service: %v", err)
	}

	until(t, "waited for everything the service started to be gone", func() bool {
		return syscall.Kill(-group, 0) == syscall.ESRCH
	})
}

func TestAServiceThatEndsOnItsOwnHandsBackTheCodeItEndedWith(t *testing.T) {
	shell(t)

	child, err := processrepo.New().Start(context.Background(), repository.Launch{
		Command: []string{"sh", "-c", "exit 7"},
	})
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	code, err := child.Wait()
	if err != nil {
		t.Fatalf("wait for the service: %v", err)
	}

	if code != 7 {
		t.Fatalf("the service ended with %d rather than the 7 it chose", code)
	}
}

func TestAStepThatOverrunsIsKilledAndSaysTheTimeRanOut(t *testing.T) {
	shell(t)

	held := &recorder{}

	code, err := processrepo.New().Run(context.Background(), repository.Launch{
		Command: []string{"sh", "-c", "echo working; sleep 300"},
		Output:  held,
	}, 200*time.Millisecond)

	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("a step given no time at all answered %d, %v", code, err)
	}

	if !strings.Contains(held.String(), "working") {
		t.Fatalf("what the step wrote before it was killed was lost: %q", held.String())
	}
}

func TestAStepHandsBackWhatItWroteAndTheCodeItEndedWith(t *testing.T) {
	shell(t)

	held := &recorder{}

	code, err := processrepo.New().Run(context.Background(), repository.Launch{
		Command: []string{"sh", "-c", "echo built; exit 3"},
		Output:  held,
	}, time.Minute)
	if err != nil {
		t.Fatalf("run a step: %v", err)
	}

	if code != 3 {
		t.Fatalf("the step ended with %d rather than the 3 it chose", code)
	}

	if !strings.Contains(held.String(), "built") {
		t.Fatalf("the step's output came back as %q", held.String())
	}
}

func TestAProcessThisMachineNeverStartedIsNotMistakenForOneOfItsOwn(t *testing.T) {
	shell(t)

	processes := processrepo.New()
	ctx := context.Background()

	child, err := processes.Start(ctx, repository.Launch{
		Command: []string{"sh", "-c", "sleep 300"},
	})
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	t.Cleanup(func() { _ = child.Stop(ctx, time.Second) })

	if !processes.Stray(ctx, child.PID(), time.Now().UTC()) {
		t.Fatalf("a service this machine started was not recognised as its own")
	}

	if processes.Stray(ctx, child.PID(), time.Now().UTC().Add(-time.Hour)) {
		t.Fatalf(
			"a process that started long after the run recorded its process id was claimed as " +
				"the run's own, so a reused process id would be killed",
		)
	}
}

func until(t *testing.T, what string, done func() bool) {
	t.Helper()

	deadline := time.Now().Add(patience)

	for time.Now().Before(deadline) {
		if done() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("%s, and it never did", what)
}
