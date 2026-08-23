package execution_test

import (
	"context"
	"os"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
)

func TestARunSomebodyAskedToKeepLongerSurvivesItsOwnWindow(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Nanosecond,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      10 * time.Millisecond,
	})
	reviewable(h)

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")
	h.awaitReview(t, "exec-01ABC")

	if err := h.service.Retain(
		context.Background(), "exec-01ABC", time.Now().UTC().Add(time.Hour),
	); err != nil {
		t.Fatalf("keep the run longer: %v", err)
	}

	approved(t, h, "exec-01ABC")
	h.awaitState(t, "exec-01ABC", channelv1.StateCompleted)

	time.Sleep(80 * time.Millisecond)

	if _, err := os.Stat(workspace(h, "exec-01ABC")); err != nil {
		t.Fatalf(
			"a run somebody asked to keep longer was cleared away on its ordinary window "+
				"anyway, so the preview they asked to go on looking at stopped resolving: %v",
			err,
		)
	}
}

func TestADeadlineThisMachineIsAlreadyPastIsIgnoredRatherThanShorteningTheWindow(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Hour,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      10 * time.Millisecond,
	})
	reviewable(h)

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")
	h.awaitReview(t, "exec-01ABC")

	if err := h.service.Retain(
		context.Background(), "exec-01ABC", time.Now().UTC().Add(-time.Hour),
	); err != nil {
		t.Fatalf("ask this machine to keep the run until a time already past: %v", err)
	}

	approved(t, h, "exec-01ABC")
	h.awaitState(t, "exec-01ABC", channelv1.StateCompleted)

	time.Sleep(50 * time.Millisecond)

	if _, err := os.Stat(workspace(h, "exec-01ABC")); err != nil {
		t.Fatalf(
			"a deadline in the past cut the run's own window short: %v. Whoever asked meant to "+
				"keep it for longer, not to end it early",
			err,
		)
	}
}

func TestADeadlineThisMachineWasGivenSurvivesARestart(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Nanosecond,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      time.Hour,
	})
	reviewable(h)

	stop := h.start(t)

	begun(t, h, "exec-01ABC")
	h.awaitReview(t, "exec-01ABC")

	if err := h.service.Retain(
		context.Background(), "exec-01ABC", time.Now().UTC().Add(time.Hour),
	); err != nil {
		t.Fatalf("keep the run longer: %v", err)
	}

	approved(t, h, "exec-01ABC")
	h.awaitState(t, "exec-01ABC", channelv1.StateCompleted)
	stop()

	after := newHarnessOverKeeping(t, h, config.Retention{
		WorkspaceAfterDone: time.Nanosecond,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      10 * time.Millisecond,
	})

	restarted := after.start(t)
	defer restarted()

	time.Sleep(80 * time.Millisecond)

	if _, err := os.Stat(workspace(after, "exec-01ABC")); err != nil {
		t.Fatalf(
			"a machine that restarted gave the workspace back inside a deadline somebody had "+
				"asked for: %v. The deadline is on disk exactly so a restart keeps to it",
			err,
		)
	}
}
