package execution_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func TestAnAcceptedOfferHoldsItsSlotOnlyOnceItIsActuallyPreparing(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if accepted := h.only(t, channelv1.ExecutionAccepted); accepted.ExecutionID != "exec-01ABC" {
		t.Fatalf("the acceptance named %q", accepted.ExecutionID)
	}

	report := h.service.Report(ctx)

	if report.Used != 0 || len(report.Executions) != 1 {
		t.Fatalf("a run that has only been accepted uses %d of %d slots", report.Used, report.Capacity)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	report = h.service.Report(ctx)

	if report.Used != 1 {
		t.Fatalf("a preparing run uses %d slots, want 1", report.Used)
	}

	if report.Executions[0].State != channelv1.StatePreparing {
		t.Fatalf("the run is %q after norn started it", report.Executions[0].State)
	}
}

func TestStartingARunMakesItsDirectoryAndSaysWhatItIsFor(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	task := filepath.Join(
		h.dir.Run("exec-01ABC"), entity.RunMetadataDir, entity.ExecutionTaskFile,
	)

	if _, err := os.Stat(task); err != nil {
		t.Fatalf("the run kept no record of what it is for: %v", err)
	}

	held, err := h.runs.LoadTasks(ctx)
	if err != nil {
		t.Fatalf("load tasks: %v", err)
	}

	if len(held) != 1 || held[0].ID != "exec-01ABC" || held[0].Title != "Execution lifecycle" {
		t.Fatalf("the record reads %+v", held)
	}

	reported := decodeInto[channelv1.Report](t, h.only(t, channelv1.ExecutionStateReport))

	if reported.State != string(channelv1.StatePreparing) {
		t.Fatalf("the machine reported %q, want preparing", reported.State)
	}
}

func TestAMachineAlreadyHoldingAllItCanTurnsTheNextOfferDown(t *testing.T) {
	h := newHarness(t, 1, 0)
	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := h.service.Offer(ctx, h.offer("exec-01DEF")); err != nil {
		t.Fatalf("second offer: %v", err)
	}

	declined := h.only(t, channelv1.ExecutionDeclined)

	if declined.ExecutionID != "exec-01DEF" {
		t.Fatalf("the decline named %q", declined.ExecutionID)
	}

	reason := decodeInto[channelv1.Decline](t, declined).Reason

	if !strings.HasPrefix(reason, channelv1.DeclineAtCapacity) {
		t.Fatalf("the decline reads %q, want it to lead with at_capacity", reason)
	}

	if !strings.Contains(reason, "1 of 1") {
		t.Fatalf("the decline does not say how full the machine is: %q", reason)
	}
}

func TestASlotThatFreesUpIsTakenByTheNextOffer(t *testing.T) {
	h := newHarness(t, 1, 0)
	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := h.service.Cancel(ctx, "exec-01ABC", "the person changed their mind"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if used := h.service.Report(ctx).Used; used != 0 {
		t.Fatalf("a cancelled run still holds %d slots", used)
	}

	if err := h.service.Offer(ctx, h.offer("exec-01DEF")); err != nil {
		t.Fatalf("second offer: %v", err)
	}

	for _, message := range h.spooled(t) {
		if message.Type == channelv1.ExecutionDeclined {
			t.Fatalf("work was turned down after a slot freed up")
		}
	}

	if accepted := h.only(t, channelv1.ExecutionAccepted); accepted.ExecutionID == "" {
		t.Fatalf("nothing was accepted after a slot freed up")
	}
}

func TestBelowTheWatermarkAMachineTurnsWorkDownAndSaysSo(t *testing.T) {
	h := newHarness(t, 4, 10<<30)
	h.free = 1 << 30

	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	reason := decodeInto[channelv1.Decline](t, h.only(t, channelv1.ExecutionDeclined)).Reason

	if !strings.HasPrefix(reason, channelv1.DeclineDiskPressure) {
		t.Fatalf("the decline reads %q, want it to lead with disk_pressure", reason)
	}

	report := h.service.Report(ctx)

	if !report.Room.Pressed() || !report.Room.Known {
		t.Fatalf("the machine reports room %+v, want it to say the disk is too full", report.Room)
	}
}

func TestADiskThatCannotBeReadNeverStrandsWork(t *testing.T) {
	h := newHarness(t, 4, 10<<30)
	h.freeErr = os.ErrPermission

	report := h.service.Report(context.Background())

	if report.Room.Known || report.Room.Pressed() {
		t.Fatalf("an unreadable disk reported %+v, want it left unknown", report.Room)
	}
}

func TestAPausedMachineTakesNoWorkUntilItIsResumed(t *testing.T) {
	h := newHarness(t, 4, 0)
	ctx := context.Background()

	h.service.Pause()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	reason := decodeInto[channelv1.Decline](t, h.only(t, channelv1.ExecutionDeclined)).Reason

	if !strings.HasPrefix(reason, channelv1.DeclinePaused) {
		t.Fatalf("the decline reads %q, want it to lead with paused", reason)
	}

	if !h.service.Report(ctx).Paused {
		t.Fatalf("the machine does not say it is paused")
	}

	h.service.Resume()

	if err := h.service.Offer(ctx, h.offer("exec-01DEF")); err != nil {
		t.Fatalf("second offer: %v", err)
	}

	if accepted := h.only(t, channelv1.ExecutionAccepted); accepted.ExecutionID != "exec-01DEF" {
		t.Fatalf("a resumed machine accepted %q", accepted.ExecutionID)
	}
}

func TestNornMayChangeHowMuchAMachineHoldsWithoutRestartingIt(t *testing.T) {
	h := newHarness(t, 4, 0)
	ctx := context.Background()

	raised := 1
	h.service.Configure(channelv1.Configuration{Capacity: &raised})

	if capacity := h.service.Report(ctx).Capacity; capacity != 1 {
		t.Fatalf("the machine holds %d after being reconfigured, want 1", capacity)
	}

	ignored := 0
	h.service.Configure(channelv1.Configuration{Capacity: &ignored})

	if capacity := h.service.Report(ctx).Capacity; capacity != 1 {
		t.Fatalf("a capacity of nothing was accepted and left the machine at %d", capacity)
	}
}

func TestAnOfferSentTwiceIsAcceptedOnce(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	for range 2 {
		if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
			t.Fatalf("offer: %v", err)
		}
	}

	accepted := 0

	for _, message := range h.spooled(t) {
		if message.Type == channelv1.ExecutionAccepted {
			accepted++
		}
	}

	if accepted != 1 {
		t.Fatalf("the same offer was accepted %d times", accepted)
	}
}

func TestARunNornHasGivenUpOnIsClearedAway(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := h.service.Reconcile(ctx, []string{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if held := h.service.Report(ctx).Executions; len(held) != 0 {
		t.Fatalf("the machine still holds %+v after norn gave up on it", held)
	}

	workspace := filepath.Join(h.dir.Run("exec-01ABC"), entity.RunWorkspaceDir)

	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("the workspace was not given back: %v", err)
	}

	task := filepath.Join(h.dir.Run("exec-01ABC"), entity.RunMetadataDir, entity.ExecutionTaskFile)

	if _, err := os.Stat(task); err != nil {
		t.Fatalf("the record of what the run was for went with it: %v", err)
	}
}

func TestARunNornStillExpectsButTheMachineLostIsFailedRatherThanLeftHanging(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	if err := h.service.Reconcile(ctx, []string{"exec-01GONE"}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	reported := h.only(t, channelv1.ExecutionStateReport)

	if reported.ExecutionID != "exec-01GONE" {
		t.Fatalf("the machine reported on %q", reported.ExecutionID)
	}

	held := decodeInto[channelv1.Report](t, reported)

	if held.State != string(channelv1.StateFailed) {
		t.Fatalf("a run the machine cannot find was reported %q", held.State)
	}

	if !strings.Contains(held.Reason, "no longer has the workspace") {
		t.Fatalf("the failure does not say why: %q", held.Reason)
	}
}

func TestAMachineThatRestartedMidRunSaysTheRunWasInterruptedAndGivesTheBranchesBack(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	fabricate(t, h, "exec-01ABC", channelv1.StateRunning)

	restarted := newHarnessOver(t, h, 2, 0)
	settled := restarted.start(t)

	defer settled()

	restarted.awaitNote(t, "restarted while the run was under way")

	if held := restarted.service.Report(ctx).Executions; len(held) != 0 {
		t.Fatalf("a machine that restarted still holds %+v", held)
	}

	restarted.await(t, "waited for the workspace to be given back", func() bool {
		_, err := os.Stat(filepath.Join(h.dir.Run("exec-01ABC"), entity.RunWorkspaceDir))

		return os.IsNotExist(err)
	})

	restarted.await(t, "waited for the run to be written down as interrupted", func() bool {
		found, err := restarted.service.List(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		return len(found) == 1 && found[0].State == channelv1.StateInterrupted
	})

	for _, reported := range restarted.reports(t) {
		if reported.State == string(channelv1.StateInterrupted) {
			t.Fatalf("the machine claimed a state that is norn's to give")
		}
	}
}

func TestARunNornStillExpectsAfterARestartIsFailedSoItCanBeStartedAgain(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	fabricate(t, h, "exec-01ABC", channelv1.StateRunning)

	restarted := newHarnessOver(t, h, 2, 0)
	settled := restarted.start(t)

	defer settled()

	restarted.awaitNote(t, "restarted while the run was under way")

	if err := restarted.service.Reconcile(ctx, []string{"exec-01ABC"}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	failed := false

	for _, reported := range restarted.reports(t) {
		if reported.State == string(channelv1.StateFailed) {
			failed = true
		}
	}

	if !failed {
		t.Fatalf("norn was never told the run this machine lost is over")
	}
}

func TestPreparingCopiesTheConnectedFolderIntoTheRunAndSaysWhatItSetUp(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.awaitNote(t, "workspace for this run is ready")

	requests := h.requests()

	if len(requests) != 1 {
		t.Fatalf("the folder was copied %d times", len(requests))
	}

	if requests[0].Run != "exec-01ABC" || requests[0].Path != "/codebase" {
		t.Fatalf("the snapshot was taken as %+v", requests[0])
	}

	setup, err := h.runs.LoadSetup(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("read what the run was set up with: %v", err)
	}

	if setup.Permissions.Profile != entity.ProfileStandard {
		t.Fatalf("the run is under the %q profile", setup.Permissions.Profile)
	}

	if setup.Driver.Kind != entity.DriverClaude || !setup.Driver.Installed {
		t.Fatalf("the run names the driver as %+v", setup.Driver)
	}

	if setup.Services.Runtime != entity.RuntimeProcess {
		t.Fatalf("the run resolved to the %q runtime", setup.Services.Runtime)
	}

	if setup.Plan.Source != entity.PlanNone {
		t.Fatalf("a run plan was found where the folder has none: %+v", setup.Plan)
	}

	if _, err := h.runs.Load(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("the run kept no record of what it copied: %v", err)
	}
}

func TestARunOnAMachineWithNoConnectedFolderFailsSayingSo(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.connected = nil

	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.await(t, "waited for the run to fail", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(channelv1.StateFailed) &&
				strings.Contains(reported.Reason, "no connected folder") {
				return true
			}
		}

		return false
	})

	if used := h.service.Report(ctx).Used; used != 0 {
		t.Fatalf("a failed run still holds %d slots", used)
	}
}

func TestAMachineWithSeveralConnectedFoldersSaysItCannotChooseBetweenThem(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.connected = []entity.Codebase{connected("/one"), connected("/two")}

	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.await(t, "waited for the run to fail", func() bool {
		for _, reported := range h.reports(t) {
			if strings.Contains(reported.Reason, "more than one connected folder") &&
				strings.Contains(reported.Reason, "it has 2") {
				return true
			}
		}

		return false
	})
}

func TestAFailureWhileCopyingTheFolderNamesItsCauseOnTheTimeline(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.takeErr = fmt.Errorf("%w: runner", entity.ErrSnapshotDirtyConflict)

	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.await(t, "waited for the run to fail", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State != string(channelv1.StateFailed) {
				continue
			}

			if strings.Contains(reported.Reason, "copy that folder into a workspace") &&
				strings.Contains(reported.Reason, "do not apply onto the base commit") {
				return true
			}
		}

		return false
	})
}

func TestCancellingARunPartWayThroughStopsTheWorkAndGivesTheBranchesBack(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.linger = patience

	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.await(t, "waited for the folder to start being copied", func() bool {
		return len(h.requests()) == 1
	})

	if err := h.service.Cancel(ctx, "exec-01ABC", "the person changed their mind"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	h.await(t, "waited for the workspace to be given back", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()

		return len(h.released) > 0
	})

	if used := h.service.Report(ctx).Used; used != 0 {
		t.Fatalf("a cancelled run still holds %d slots", used)
	}

	for _, reported := range h.reports(t) {
		if reported.State == string(channelv1.StateFailed) {
			t.Fatalf("a run the person cancelled was reported as a failure")
		}
	}
}

func TestARunStartedAgainAfterAnInterruptionTakesUpTheBranchTheLastOneLeft(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.awaitNote(t, "workspace for this run is ready")

	again := h.offer("exec-01DEF")
	again.Attempt = 2
	again.Reference = "NORN-47-r2"

	if err := h.service.Offer(ctx, again); err != nil {
		t.Fatalf("second offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01DEF", channelv1.Start{ExecutionID: "exec-01DEF"}); err != nil {
		t.Fatalf("second start: %v", err)
	}

	h.await(t, "waited for the second attempt to be prepared", func() bool {
		return len(h.requests()) == 2
	})

	reused := h.requests()[1].Branches

	if reused["runner"] != entity.BranchFor("NORN-47", "runner", 1) {
		t.Fatalf("the second attempt asked for %q, want the first attempt's branch", reused["runner"])
	}
}

func TestAFreshAttemptWithNothingToPickUpGetsABranchOfItsOwn(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	again := h.offer("exec-01DEF")
	again.Attempt = 2

	if err := h.service.Offer(ctx, again); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01DEF", channelv1.Start{ExecutionID: "exec-01DEF"}); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.await(t, "waited for the run to be prepared", func() bool {
		return len(h.requests()) == 1
	})

	if branches := h.requests()[0].Branches; len(branches) != 0 {
		t.Fatalf("a first attempt on this machine asked to reuse %+v", branches)
	}
}

func TestARunKeepsItsOwnTimelineOnDiskSoItReadsWithoutNorn(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.awaitNote(t, "workspace for this run is ready")

	timeline, err := h.service.Timeline(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("read the run's own timeline: %v", err)
	}

	states := 0
	phases := 0

	for _, entry := range timeline {
		if entry.State == channelv1.StatePreparing {
			states++
		}

		if entry.Kind == channelv1.EventPhase {
			phases++
		}
	}

	if states != 1 || phases < 2 {
		t.Fatalf("the run's own timeline reads %+v", timeline)
	}
}

func TestWhatTheMachineSaysHelloWithNamesTheBuildAndWhatItHolds(t *testing.T) {
	h := newHarness(t, 3, 0)
	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	hello := h.service.Greeting()

	if hello.Version != "1.4.0" || hello.Protocol != entity.ChannelProtocol {
		t.Fatalf("the greeting reads %+v", hello)
	}

	if hello.Capacity != 3 || len(hello.Executions) != 1 || hello.Executions[0] != "exec-01ABC" {
		t.Fatalf("the greeting does not say what the machine holds: %+v", hello)
	}

	pulse := h.service.Pulse(ctx)

	if pulse.Capacity != 3 || pulse.Used != 0 || len(pulse.Phases) != 1 {
		t.Fatalf("the heartbeat reads %+v", pulse)
	}

	if pulse.Phases[0].State != string(channelv1.StateLeased) {
		t.Fatalf("the heartbeat reports the run as %q", pulse.Phases[0].State)
	}
}

func TestAStartForSomethingTheMachineNeverAcceptedIsFailedNotInvented(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	if err := h.service.Start(ctx, "exec-01GONE", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	held := decodeInto[channelv1.Report](t, h.only(t, channelv1.ExecutionStateReport))

	if held.State != string(channelv1.StateFailed) {
		t.Fatalf("a start for an unknown run was reported %q", held.State)
	}

	if len(h.service.Report(ctx).Executions) != 0 {
		t.Fatalf("the machine invented a run it was never offered")
	}
}

func TestOnceTheCodingAgentHasFinishedTheRunWaitsForReviewAndGivesItsSlotBack(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	stop := h.start(t)
	defer stop()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.awaitReview(t, "exec-01ABC")

	report := h.service.Report(ctx)

	if len(report.Executions) != 1 ||
		report.Executions[0].State != channelv1.StateAwaitingReview {
		t.Fatalf("a run with nothing left to do reads %+v", report.Executions)
	}

	if report.Used != 0 {
		t.Fatalf(
			"a run waiting for a person to review it still holds %d of this machine's %d slots; "+
				"nothing is running, so the machine would turn work away for no reason",
			report.Used, report.Capacity,
		)
	}
}

func TestARunDirectoryLeftBehindByAnEarlierMachineIsPickedUpAndFinished(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	fabricate(t, h, "exec-01OLD", channelv1.StateRunning)

	stop := h.start(t)
	defer stop()

	h.awaitNote(t, "restarted while the run was under way")

	h.await(t, "waited for the run to be written down as interrupted", func() bool {
		held, err := h.service.List(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		return len(held) == 1 && held[0].State == channelv1.StateInterrupted
	})

	if used := h.service.Report(ctx).Used; used != 0 {
		t.Fatalf("a run left behind by an earlier machine still holds %d slots", used)
	}

	h.await(t, "waited for the workspace of the fabricated run to be given back", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()

		return len(h.released) == 1
	})

	timeline, err := h.service.Timeline(ctx, "exec-01OLD")
	if err != nil {
		t.Fatalf("read the fabricated run's timeline: %v", err)
	}

	if len(timeline) == 0 {
		t.Fatalf("nothing was written down about a run that was interrupted")
	}
}

func fabricate(t *testing.T, h *harness, executionID string, state channelv1.State) {
	t.Helper()

	ctx := context.Background()

	if _, err := h.runs.Open(ctx, executionID); err != nil {
		t.Fatalf("make a run directory by hand: %v", err)
	}

	execution := entity.Execution{
		ID:         executionID,
		Reference:  "NORN-47",
		IssueKey:   "NORN-47",
		Attempt:    1,
		Title:      "Execution lifecycle",
		Directory:  h.dir.Run(executionID),
		State:      state,
		AcceptedAt: time.Now().UTC().Add(-time.Hour),
		StartedAt:  time.Now().UTC().Add(-time.Hour),
	}

	if err := h.runs.SaveTask(ctx, execution); err != nil {
		t.Fatalf("write a task by hand: %v", err)
	}

	if err := h.runs.Save(ctx, entity.Snapshot{
		Name:      executionID,
		IssueKey:  "NORN-47",
		Attempt:   1,
		Workspace: filepath.Join(h.dir.Run(executionID), entity.RunWorkspaceDir),
		Repositories: []entity.SnapshotRepository{{
			Name:   "runner",
			Mode:   entity.GitModeWorktree,
			Branch: entity.BranchFor("NORN-47", "runner", 1),
		}},
		TakenAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("write a snapshot record by hand: %v", err)
	}
}
