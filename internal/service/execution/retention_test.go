package execution_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	schedulingrepo "github.com/usenorn/runner/internal/repository/scheduling"
)

func approved(t *testing.T, h *harness, executionID string) {
	t.Helper()

	if err := h.service.Continue(context.Background(), executionID, channelv1.Instruction{
		Reason: channelv1.ResumeApproved,
	}); err != nil {
		t.Fatalf("approve %s: %v", executionID, err)
	}
}

func reviewable(h *harness) {
	working(h)

	h.drivers.scripts = []script{finishes("session-01", "added a median helper")}

	h.posts.EXPECT().
		PublishArtifact(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.ArtifactReceipt{ID: "f8b0a1c2-0000-4000-8000-000000000001"}, nil).
		AnyTimes()
}

func workspace(h *harness, executionID string) string {
	return filepath.Join(h.dir.Run(executionID), entity.RunWorkspaceDir)
}

func TestApprovingARunFinishesItRatherThanSettingTheCodingAgentGoingAgain(t *testing.T) {
	h := newHarness(t, 2, 0)
	reviewable(h)

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")
	h.awaitReview(t, "exec-01ABC")

	approved(t, h, "exec-01ABC")

	h.awaitState(t, "exec-01ABC", channelv1.StateCompleted)

	if carried := h.drivers.carried(); len(carried) != 0 {
		t.Fatalf(
			"approving the work started the coding agent again %d times. Norn has already moved "+
				"the run to approved, so the running this machine would then report is a move "+
				"norn refuses, and a refused message closes the channel",
			len(carried),
		)
	}

	for _, reported := range h.reports(t) {
		if reported.State == string(channelv1.StateApproved) {
			t.Fatalf(
				"this machine reported approved, which is norn's move to make and not one a " +
					"runner may claim",
			)
		}
	}
}

func TestAnApprovedRunKeepsItsWorkspaceUntilTheWindowHasPassed(t *testing.T) {
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
	approved(t, h, "exec-01ABC")
	h.awaitState(t, "exec-01ABC", channelv1.StateCompleted)

	h.awaitNote(t, "kept here for 1h0m0s")

	time.Sleep(50 * time.Millisecond)

	if _, err := os.Stat(workspace(h, "exec-01ABC")); err != nil {
		t.Fatalf(
			"the workspace of an approved run was cleared away inside its window, so a person "+
				"following the link found nothing there: %v",
			err,
		)
	}
}

func TestOnceTheWindowHasPassedTheWorkspaceGoesBackAndWhatExplainsTheRunStays(t *testing.T) {
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
	approved(t, h, "exec-01ABC")

	h.await(t, "waited for the workspace to be given back", func() bool {
		_, err := os.Stat(workspace(h, "exec-01ABC"))

		return os.IsNotExist(err)
	})

	if _, err := os.Stat(filepath.Join(
		h.dir.Run("exec-01ABC"), entity.RunMetadataDir, entity.ExecutionTaskFile,
	)); err != nil {
		t.Fatalf(
			"giving the workspace back took what the run was with it, so the run can no longer "+
				"say what it did: %v",
			err,
		)
	}

	timeline, err := h.service.Timeline(context.Background(), "exec-01ABC")
	if err != nil {
		t.Fatalf("read the run's own timeline: %v", err)
	}

	if len(timeline) == 0 {
		t.Fatalf("the run's timeline went with its workspace")
	}
}

func TestARunSomebodyStoppedIsAlsoGivenBackOnceItsWindowHasPassed(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Nanosecond,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      10 * time.Millisecond,
	})

	h.drivers.scripts = []script{holds("session-01")}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitState(t, "exec-01ABC", channelv1.StateRunning)

	if err := h.service.Cancel(
		context.Background(), "exec-01ABC", "a person stopped it",
	); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	h.await(t, "waited for the stopped run's workspace to be given back", func() bool {
		_, err := os.Stat(workspace(h, "exec-01ABC"))

		return os.IsNotExist(err)
	})
}

func TestARunSomebodyStoppedNeverReportsAnythingAfterwards(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Hour,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      time.Hour,
	})

	h.drivers.scripts = []script{holds("session-01")}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")
	h.awaitState(t, "exec-01ABC", channelv1.StateRunning)

	if err := h.service.Cancel(
		context.Background(), "exec-01ABC", "a person stopped it",
	); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	h.awaitNote(t, "kept here for")

	held, err := h.runs.LoadTask(context.Background(), "exec-01ABC")
	if err != nil {
		t.Fatalf("read back what the run ended as: %v", err)
	}

	if held.State != channelv1.StateCancelled || held.SettledAt.IsZero() {
		t.Fatalf(
			"a cancelled run was written down as %s settled %s, because the turn that was still "+
				"running carried on and reported over the top of the cancel. Norn refuses those "+
				"moves, and the machine is then left holding a workspace nothing will ever collect",
			held.State, held.SettledAt,
		)
	}

	for _, reported := range h.reports(t) {
		if reported.State == string(channelv1.StateAwaitingReview) {
			t.Fatalf("a run somebody stopped told norn it was waiting for review")
		}
	}
}

func TestTheSweepNeverTouchesARunThatIsWaitingForSomebodyToReviewIt(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Nanosecond,
		RunsMaxAge:         time.Nanosecond,
		RunsMaxDisk:        1,
		SweepInterval:      10 * time.Millisecond,
	})

	fabricate(t, h, "exec-01ABC", channelv1.StateAwaitingReview)

	stop := h.start(t)
	defer stop()

	h.await(t, "waited for the machine to pick the run back up", func() bool {
		return len(h.service.Report(context.Background()).Executions) == 1
	})

	time.Sleep(60 * time.Millisecond)

	if _, err := os.Stat(workspace(h, "exec-01ABC")); err != nil {
		t.Fatalf(
			"the sweep took the workspace of a run this machine is still holding for review: %v",
			err,
		)
	}
}

func TestTheSweepLeavesASnapshotSomebodyTookByHandAlone(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Nanosecond,
		RunsMaxAge:         time.Nanosecond,
		RunsMaxDisk:        1,
		SweepInterval:      10 * time.Millisecond,
	})

	if _, err := h.runs.Open(context.Background(), "snap-NORN-55-1"); err != nil {
		t.Fatalf("take a snapshot by hand: %v", err)
	}

	stop := h.start(t)
	defer stop()

	time.Sleep(60 * time.Millisecond)

	if _, err := os.Stat(h.dir.Run("snap-NORN-55-1")); err != nil {
		t.Fatalf(
			"the sweep deleted a snapshot somebody took by hand; it is theirs to remove with "+
				"'norn runner snapshot remove': %v",
			err,
		)
	}
}

func TestARunPastTheAgeThisMachineKeepsIsTakenOffTheDiskWhole(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Hour,
		RunsMaxAge:         time.Minute,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      10 * time.Millisecond,
	})

	settle(t, h, "exec-01OLD", time.Now().UTC().Add(-time.Hour))

	stop := h.start(t)
	defer stop()

	h.await(t, "waited for the old run to be taken off the disk", func() bool {
		_, err := os.Stat(h.dir.Run("exec-01OLD"))

		return os.IsNotExist(err)
	})
}

func TestWhenTheDiskIsOverBudgetTheOldestFinishedRunGoesAndTheNewestStays(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Hour,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        6000,
		SweepInterval:      10 * time.Millisecond,
	})

	now := time.Now().UTC()

	settle(t, h, "exec-01OLD", now.Add(-time.Hour))
	settle(t, h, "exec-01NEW", now.Add(-time.Minute))

	for _, id := range []string{"exec-01OLD", "exec-01NEW"} {
		if err := os.WriteFile(
			filepath.Join(workspace(h, id), "big"), make([]byte, 4096), 0o600,
		); err != nil {
			t.Fatalf("fill %s: %v", id, err)
		}
	}

	stop := h.start(t)
	defer stop()

	h.await(t, "waited for the oldest run to be taken off the disk", func() bool {
		_, err := os.Stat(h.dir.Run("exec-01OLD"))

		return os.IsNotExist(err)
	})

	if _, err := os.Stat(h.dir.Run("exec-01NEW")); err != nil {
		t.Fatalf(
			"the newest run went too, so the machine threw away more history than its budget "+
				"asked for: %v",
			err,
		)
	}
}

func TestWhatTheRunsOnThisMachineTakeUpIsOnItsStatus(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Hour,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      10 * time.Millisecond,
	})

	settle(t, h, "exec-01ABC", time.Now().UTC())

	if err := os.WriteFile(
		filepath.Join(workspace(h, "exec-01ABC"), "big"), make([]byte, 4096), 0o600,
	); err != nil {
		t.Fatalf("fill the run: %v", err)
	}

	stop := h.start(t)
	defer stop()

	h.await(t, "waited for the machine to measure what its runs take up", func() bool {
		runs := h.service.Report(context.Background()).Runs

		return runs.Runs == 1 && runs.Bytes >= 4096 && !runs.SweptAt.IsZero()
	})
}

func TestOnceARunIsClearedAwayTheMachineStopsCountingWhatItUsedToTakeUp(t *testing.T) {
	h := newHarnessKeeping(t, config.Retention{
		WorkspaceAfterDone: time.Nanosecond,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      time.Hour,
	})

	settle(t, h, "exec-01ABC", time.Now().UTC().Add(-time.Hour))

	if err := os.WriteFile(
		filepath.Join(workspace(h, "exec-01ABC"), "big"), make([]byte, 1<<20), 0o600,
	); err != nil {
		t.Fatalf("fill the run: %v", err)
	}

	stop := h.start(t)
	defer stop()

	h.await(t, "waited for the workspace to be given back", func() bool {
		_, err := os.Stat(workspace(h, "exec-01ABC"))

		return os.IsNotExist(err)
	})

	h.await(t, "waited for the one sweep to finish", func() bool {
		return !h.service.Report(context.Background()).Runs.SweptAt.IsZero()
	})

	if held := h.service.Report(context.Background()).Runs.Bytes; held >= 1<<20 {
		t.Fatalf(
			"the sweep that gave a workspace back still counts its %d bytes, so status overstates "+
				"the disk until the next sweep — which on the default settings is five minutes of "+
				"telling a person their machine is fuller than it is",
			held,
		)
	}

}

func TestAMachineSomebodyPausedComesBackPaused(t *testing.T) {
	h := newHarness(t, 2, 0)

	if err := schedulingrepo.New(h.dir).Pause(context.Background(), true); err != nil {
		t.Fatalf("pause the machine: %v", err)
	}

	over := newHarnessOver(t, h, 2, 0)

	stop := over.start(t)
	defer stop()

	over.await(t, "waited for the machine to read back that it was paused", func() bool {
		return over.service.Report(context.Background()).Paused
	})

	if err := over.service.Offer(context.Background(), over.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	declined := over.only(t, channelv1.ExecutionDeclined)
	reason := decodeInto[channelv1.Decline](t, declined)

	if reason.Code != channelv1.DeclinePaused {
		t.Fatalf(
			"a machine somebody paused took work again after a restart, answering %q",
			reason.Code,
		)
	}
}

func TestARunThisMachineHasAlreadyFinishedIsNotReportedFailedWhenNornCatchesUp(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	settle(t, h, "exec-01ABC", time.Now().UTC())

	if err := h.service.Reconcile(ctx, []string{"exec-01ABC"}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, reported := range h.reports(t) {
		if reported.State == string(channelv1.StateFailed) {
			t.Fatalf(
				"a sync that arrived before norn had read this machine's own report made it " +
					"claim failed from a state norn has since made terminal. Norn refuses that " +
					"move, and a message it refuses closes the channel rather than being answered",
			)
		}
	}
}

func TestARunThisMachineOnlyMarkedInterruptedIsStillReportedWhenNornCatchesUp(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	fabricate(t, h, "exec-01ABC", channelv1.StateInterrupted)

	if err := h.service.Reconcile(ctx, []string{"exec-01ABC"}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	failed := false

	for _, reported := range h.reports(t) {
		if reported.State == string(channelv1.StateFailed) {
			failed = true
		}
	}

	if !failed {
		t.Fatalf(
			"a run this machine marked interrupted after a restart was never reported, so norn " +
				"holds a lease nothing will settle. Interrupted is not a state a runner may claim, " +
				"which is why the sync has to say failed instead",
		)
	}
}

func settle(t *testing.T, h *harness, executionID string, at time.Time) {
	t.Helper()

	fabricate(t, h, executionID, channelv1.StateCompleted)

	ctx := context.Background()
	runs := runrepo.New(h.dir)

	execution, err := runs.LoadTask(ctx, executionID)
	if err != nil {
		t.Fatalf("read back the run just written: %v", err)
	}

	execution.SettledAt = at

	if err := runs.SaveTask(ctx, execution); err != nil {
		t.Fatalf("write down when the run settled: %v", err)
	}
}
