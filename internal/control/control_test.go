package control_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
	runrepo "github.com/usenorn/runner/internal/repository/run"
)

func TestStatusAnswersOverTheSocketWithWhatTheRunnerIs(t *testing.T) {
	h := newHarness(t, nil)

	status, err := h.client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status.PID != os.Getpid() {
		t.Fatalf("status reported pid %d, want this process %d", status.PID, os.Getpid())
	}

	if status.Socket != h.dir.Socket() {
		t.Fatalf("status reported socket %q, want %q", status.Socket, h.dir.Socket())
	}

	if status.Capacity != 4 || status.Runtime != string(config.RuntimeAuto) {
		t.Fatalf("status did not carry the configuration: %+v", status)
	}

	if status.StartedAt.IsZero() {
		t.Fatalf("status reported no start time")
	}
}

func TestAFreshRunnerReportsItselfUnenrolledUntilAnIdentityExists(t *testing.T) {
	h := newHarness(t, nil)

	status, err := h.client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status.Enrolled {
		t.Fatalf("a runner with no identity file reported itself enrolled")
	}
}

func TestAskingAnUnknownPathIsRefused(t *testing.T) {
	h := newHarness(t, http.NewServeMux())

	if _, err := h.client.Status(context.Background()); err == nil {
		t.Fatalf("an unrouted path answered as though it were status")
	}
}

func TestStatusWithNoRunnerListeningFailsAtOnceAndSaysHowToStartOne(t *testing.T) {
	dir := newStateDir(t)
	client := control.NewClient(settings(), questionSettings(), dir, "")

	started := time.Now()

	_, err := client.Status(context.Background())
	if err == nil {
		t.Fatalf("status answered with no daemon running")
	}

	if code := entity.ExitCode(err); code != entity.ExitDaemonUnavailable {
		t.Fatalf("exit code is %d, want %d so a script can tell this apart from a real failure",
			code, entity.ExitDaemonUnavailable)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("status took %s to notice nothing was listening; it must not hang", elapsed)
	}
}

func TestARunnerThatAcceptsButNeverAnswersIsGivenUpOn(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+control.StatusPath, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	h := newHarness(t, mux)

	started := time.Now()

	if _, err := h.client.Status(context.Background()); err == nil {
		t.Fatalf("a daemon that never answered was treated as success")
	}

	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("giving up took %s, longer than the request timeout allows", elapsed)
	}
}

func TestTheDaemonSaysWhichBuildItIsRunningAndWhatItKnowsAboutReleases(t *testing.T) {
	h := newHarness(t, nil)

	build, err := h.client.Version(context.Background())
	if err != nil {
		t.Fatalf("version: %v", err)
	}

	if build.Version != h.build.Version {
		t.Fatalf("the daemon reports build %q, want the one it was started from", build.Version)
	}

	if build.OS != h.build.OS || build.Arch != h.build.Arch {
		t.Fatalf("the daemon reports %s/%s, want its own platform", build.OS, build.Arch)
	}

	if build.Update.State == "" {
		t.Fatalf("the daemon says nothing about updates, so status has no row to show")
	}
}

func TestStatusCarriesTheUpdateAnswerSoNobodyHasToAskTwice(t *testing.T) {
	h := newHarness(t, nil)

	status, err := h.client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if !entity.UpdateState(status.Update.State).Valid() {
		t.Fatalf("status reported update state %q, which is not one this runner defines", status.Update.State)
	}
}

func TestInspectingAFolderThatIsNotThereSaysSoRatherThanFailing(t *testing.T) {
	h := newHarness(t, nil)

	_, err := h.client.Inspect(context.Background(), filepath.Join(h.dir.Root(), "nowhere"))
	if err == nil {
		t.Fatalf("inspecting a folder that does not exist succeeded")
	}

	if !strings.Contains(err.Error(), "no folder") {
		t.Fatalf("inspecting a missing folder answered %q, which does not say what is wrong", err)
	}
}

func TestStatusSaysWhichFoldersThisMachineHolds(t *testing.T) {
	h := newHarness(t, nil)

	status, err := h.client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if len(status.Codebases) != 0 {
		t.Fatalf("a machine with no connected folders reported %d", len(status.Codebases))
	}
}

func TestStatusSaysWhetherThisMachineIsTalkingToNornAndHowFullItIs(t *testing.T) {
	h := newHarness(t, nil)

	status, err := h.client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if !entity.ChannelState(status.Channel.State).Valid() {
		t.Fatalf("status reports the channel as %q", status.Channel.State)
	}

	if status.Scheduler.Capacity != 2 {
		t.Fatalf("status says this machine holds %d executions", status.Scheduler.Capacity)
	}

	if status.Scheduler.Used != 0 || status.Scheduler.Paused {
		t.Fatalf("a fresh machine reports %+v", status.Scheduler)
	}

	if status.Scheduler.FreeDisk == nil || *status.Scheduler.FreeDisk <= 0 {
		t.Fatalf("status does not say how much room is left for a run")
	}
}

func TestPausingAndResumingThisMachineTakesEffectWithoutARestart(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	paused, err := h.client.Pause(ctx)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}

	if !paused.Paused {
		t.Fatalf("pausing answered %+v", paused)
	}

	status, err := h.client.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if !status.Scheduler.Paused {
		t.Fatalf("status does not report the machine as paused")
	}

	resumed, err := h.client.Resume(ctx)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	if resumed.Paused {
		t.Fatalf("resuming answered %+v", resumed)
	}

	status, err = h.client.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status.Scheduler.Paused {
		t.Fatalf("status still reports the machine as paused")
	}
}

func TestTheRunsThisMachineHasTakenAreListedOverTheSocket(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	found, err := h.client.Executions(ctx)
	if err != nil {
		t.Fatalf("executions: %v", err)
	}

	if len(found) != 0 {
		t.Fatalf("a machine that has run nothing listed %+v", found)
	}

	fabricate(t, h, "exec-01ABC")

	found, err = h.client.Executions(ctx)
	if err != nil {
		t.Fatalf("executions: %v", err)
	}

	if len(found) != 1 || found[0].ID != "exec-01ABC" || found[0].IssueKey != "NORN-47" {
		t.Fatalf("the machine listed %+v", found)
	}

	if found[0].Held {
		t.Fatalf("a run left behind by an earlier daemon is being counted as held")
	}
}

func TestWhatHappenedInOneRunReadsBackOverTheSocket(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	fabricate(t, h, "exec-01ABC")

	if err := runrepo.New(h.dir).Append(ctx, "exec-01ABC", entity.TimelineEntry{
		Kind:     channelv1.EventPhase,
		State:    channelv1.StatePreparing,
		Reason:   "this run works in /codebase",
		Occurred: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write a timeline entry: %v", err)
	}

	timeline, err := h.client.Logs(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("logs: %v", err)
	}

	if len(timeline) != 1 || timeline[0].Reason != "this run works in /codebase" {
		t.Fatalf("the run's timeline came back as %+v", timeline)
	}
}

func TestAskingWhatHappenedInARunThisMachineNeverHadSaysSo(t *testing.T) {
	h := newHarness(t, nil)

	_, err := h.client.Logs(context.Background(), "exec-01GONE")
	if err == nil {
		t.Fatalf("a run this machine never had answered with a timeline")
	}

	if !strings.Contains(err.Error(), "not holding that execution") {
		t.Fatalf("the refusal reads %q", err)
	}
}

func fabricate(t *testing.T, h *harness, executionID string) {
	t.Helper()

	ctx := context.Background()
	runs := runrepo.New(h.dir)

	if _, err := runs.Open(ctx, executionID); err != nil {
		t.Fatalf("make a run directory by hand: %v", err)
	}

	if err := runs.SaveTask(ctx, entity.Execution{
		ID:         executionID,
		Reference:  "NORN-47",
		IssueKey:   "NORN-47",
		Attempt:    1,
		Title:      "Execution lifecycle",
		State:      channelv1.StateInterrupted,
		AcceptedAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("write a task by hand: %v", err)
	}
}
