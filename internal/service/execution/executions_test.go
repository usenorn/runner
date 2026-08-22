package execution_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	if len(held) != 1 || held[0].ID != "exec-01ABC" || held[0].Title != "Channel client" {
		t.Fatalf("the record reads %+v", held)
	}

	reported := decodeInto[channelv1.Report](t, h.only(t, channelv1.ExecutionStateReport))

	if reported.State != string(channelv1.StatePreparing) {
		t.Fatalf("the machine reported %q, want preparing", reported.State)
	}

	note := decodeInto[channelv1.Entry](t, h.only(t, channelv1.ExecutionEvent))

	if !strings.Contains(note.Reason, "not built into this release") {
		t.Fatalf("the timeline does not say what the run is waiting for: %q", note.Reason)
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

	if _, err := os.Stat(h.dir.Run("exec-01ABC")); !os.IsNotExist(err) {
		t.Fatalf("the run directory is still there: %v", err)
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

func TestARestartedMachinePicksUpTheRunsItWasHolding(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer("exec-01ABC")); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	restarted := newHarnessOver(t, h, 2, 0)

	if err := restarted.service.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}

	report := restarted.service.Report(ctx)

	if len(report.Executions) != 1 || report.Executions[0].ID != "exec-01ABC" {
		t.Fatalf("a restarted machine holds %+v", report.Executions)
	}

	if report.Used != 1 {
		t.Fatalf("a recovered preparing run uses %d slots, want 1", report.Used)
	}

	if err := restarted.service.Reconcile(ctx, []string{"exec-01ABC"}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(restarted.service.Report(ctx).Executions) != 1 {
		t.Fatalf("reconciling dropped a run norn and the machine agree on")
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
