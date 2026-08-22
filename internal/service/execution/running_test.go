package execution_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func begun(t *testing.T, h *harness, id string) {
	t.Helper()

	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer(id)); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, id, started()); err != nil {
		t.Fatalf("start: %v", err)
	}
}

func TestARunGoesFromPreparingThroughRunningToFinalizingAndSaysWhatIsLeft(t *testing.T) {
	h := newHarness(t, 2, 0)

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitNote(t, "not built into this release")

	states := []string{}

	for _, reported := range h.reports(t) {
		states = append(states, reported.State)
	}

	for _, wanted := range []string{
		string(channelv1.StatePreparing),
		string(channelv1.StateRunning),
		string(channelv1.StateFinalizing),
	} {
		if !slices.Contains(states, wanted) {
			t.Fatalf("a run that worked reported %v, without %s", states, wanted)
		}
	}
}

func TestTheAgentIsToldAboutTheIssueAndPutInTheWorkspaceMadeForTheRun(t *testing.T) {
	h := newHarness(t, 2, 0)

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitNote(t, "not built into this release")

	tasks := h.drivers.began()

	if len(tasks) != 1 || !strings.Contains(tasks[0].Prompt, "NORN-47") {
		t.Fatalf("what the agent was asked to do reads %+v", tasks)
	}

	worked := h.drivers.worked()

	if len(worked) != 1 {
		t.Fatalf("the agent was started %d times", len(worked))
	}

	wanted := filepath.Join(h.dir.Run("exec-01ABC"), entity.RunWorkspaceDir)

	if worked[0].Workspace != wanted {
		t.Fatalf("the agent was put in %s rather than the workspace for the run", worked[0].Workspace)
	}

	if !slices.Contains(worked[0].Environment, entity.ExecutionVariable+"=exec-01ABC") {
		t.Fatalf("the agent was not told which run it is working on")
	}

	if worked[0].Profile != entity.ProfileStandard {
		t.Fatalf("the agent was given the %s profile", worked[0].Profile)
	}
}

func TestWhatTheAgentSaysAndDoesIsSentToNornAsItGoes(t *testing.T) {
	h := newHarness(t, 2, 0)

	played := finishes("session-01", "the work is committed")
	played.events = []entity.DriverEvent{
		{Kind: entity.DriverEventMessage, Text: "I will read the file first"},
		{Kind: entity.DriverEventToolCall, Tool: "Read"},
		{Kind: entity.DriverEventToolResult, Tool: "Read", Text: "the file"},
	}
	played.logs = []string{"warning: the wrapper had something to say"}

	h.drivers.scripts = []script{played}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitNote(t, "not built into this release")

	h.await(t, "waited for the transcript to reach norn", func() bool {
		return len(h.sent()) == 3
	})

	h.await(t, "waited for what the agent printed to reach norn", func() bool {
		for _, line := range h.logged() {
			if strings.Contains(line.Text, "the wrapper had something to say") {
				return true
			}
		}

		return false
	})

	h.awaitNote(t, "the coding agent used Read")
}

func TestTheSessionARunUsedIsWrittenDownSoItCanBeCarriedOnLater(t *testing.T) {
	h := newHarness(t, 2, 0)

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitNote(t, "not built into this release")

	driver, err := h.runs.LoadDriver(context.Background(), "exec-01ABC")
	if err != nil {
		t.Fatalf("read what the run was driven with: %v", err)
	}

	held, found := driver.Latest()

	if !found || held.ID != "session-01" {
		t.Fatalf("the run wrote down %+v as the session it used", driver.Sessions)
	}

	if held.Outcome != entity.OutcomeDone {
		t.Fatalf("the session was written down as %s", held.Outcome)
	}
}

func TestAnAgentThatStopsWithoutFinishingIsAskedToCarryOnFromWhereItLeftOff(t *testing.T) {
	h := newHarness(t, 2, 0)

	h.drivers.scripts = []script{
		crashes("session-01"),
		finishes("session-01", "picked it back up and finished"),
	}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitNote(t, "not built into this release")

	carried := h.drivers.carried()

	if len(carried) != 1 || carried[0].ID != "session-01" {
		t.Fatalf("what the run carried on from reads %+v", carried)
	}

	h.awaitNote(t, "asking it to carry on")
}

func TestAnAgentThatStopsWithoutFinishingTwiceFailsTheRunRatherThanTryingForever(t *testing.T) {
	h := newHarness(t, 2, 0)

	h.drivers.scripts = []script{crashes("session-01"), crashes("session-01")}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to be given up on", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(channelv1.StateFailed) {
				return strings.Contains(reported.Reason, "without saying it was finished")
			}
		}

		return false
	})

	if len(h.drivers.carried()) != 1 {
		t.Fatalf("the run carried on %d times", len(h.drivers.carried()))
	}
}

func TestACodingAgentThatIsNotSignedInFailsTheRunBeforeAWorkspaceIsCopied(t *testing.T) {
	h := newHarness(t, 2, 0)

	h.drivers.health = entity.DriverHealth{
		Kind:      entity.DriverClaude,
		Installed: true,
		Version:   "2.1.239",
		Problem:   entity.ErrDriverSignedOut.Error(),
	}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to say the agent is not signed in", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(channelv1.StateFailed) {
				return strings.Contains(reported.Reason, "claude auth login")
			}
		}

		return false
	})

	if len(h.requests()) != 0 {
		t.Fatalf("a workspace was copied for a run that could never start")
	}
}

func TestCancellingARunStopsTheAgentAndGivesTheWorkspaceBack(t *testing.T) {
	h := newHarness(t, 2, 0)

	h.drivers.scripts = []script{holds("session-01")}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the coding agent to be under way", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(channelv1.StateRunning) {
				return strings.Contains(reported.Reason, "is working on this run")
			}
		}

		return false
	})

	if err := h.service.Cancel(context.Background(), "exec-01ABC", "a person stopped it"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	h.await(t, "waited for the agent to be stopped", func() bool {
		h.drivers.mu.Lock()
		defer h.drivers.mu.Unlock()

		return h.drivers.stopped > 0
	})

	h.await(t, "waited for the workspace to be given back", func() bool {
		_, err := os.Stat(filepath.Join(h.dir.Run("exec-01ABC"), entity.RunWorkspaceDir))

		return os.IsNotExist(err)
	})
}

func TestWhatTheMachineDecidedAboutPermissionsIsWrittenDownWithTheRun(t *testing.T) {
	h := newHarness(t, 2, 0)

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitNote(t, "not built into this release")

	setup, err := h.runs.LoadSetup(context.Background(), "exec-01ABC")
	if err != nil {
		t.Fatalf("read what the run was set up with: %v", err)
	}

	if setup.Permissions.Profile != entity.ProfileStandard {
		t.Fatalf("the run was set up under the %s profile", setup.Permissions.Profile)
	}

	if !strings.Contains(setup.Permissions.Chosen, "this machine took its own default") {
		t.Fatalf("nothing says where the profile came from: %q", setup.Permissions.Chosen)
	}
}

func TestAnAgentThatStopsForSomethingOnlyItsOwnSessionCouldAnswerFailsTheRunSayingSo(t *testing.T) {
	h := newHarness(t, 2, 0)

	asked := finishes("session-01", "should I keep the old endpoint?")
	asked.result.Outcome = entity.OutcomeNeedsInput

	h.drivers.scripts = []script{asked}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to say the agent stopped for something nobody can answer", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(channelv1.StateFailed) {
				return strings.Contains(reported.Reason, "goes through 'norn ask'")
			}
		}

		return false
	})

	if len(h.drivers.carried()) != 0 {
		t.Fatalf("a run waiting on a person was carried on regardless")
	}
}
