package driver_test

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestEachProfileAsksTheCodingAgentForADifferentThing(t *testing.T) {
	for profile, wanted := range map[entity.PermissionProfile][]string{
		entity.ProfileStrict:       {"--permission-mode", "dontAsk", "--allowedTools", "Read"},
		entity.ProfileStandard:     {"--permission-mode", "auto", "--disallowedTools"},
		entity.ProfileUnrestricted: {"--permission-mode", "bypassPermissions"},
	} {
		t.Run(string(profile), func(t *testing.T) {
			h := newHarness(t)

			h.replays(t, "clean.ndjson")
			h.drain(t, h.start(t, profile))

			asked := h.asked(t)

			for _, argument := range wanted {
				if !slices.Contains(asked, argument) {
					t.Fatalf("the %s profile asked for %v, without %q", profile, asked, argument)
				}
			}
		})
	}
}

func TestTheStrictProfileLetsTheAgentReadAndRefusesEverythingThatWrites(t *testing.T) {
	h := newHarness(t)

	h.replays(t, "clean.ndjson")
	h.drain(t, h.start(t, entity.ProfileStrict))

	asked := h.asked(t)

	for _, refused := range []string{"Write", "Edit", "Bash"} {
		if slices.Contains(asked, refused) {
			t.Fatalf("a strict session was allowed %s: %v", refused, asked)
		}
	}

	if slices.Contains(asked, "bypassPermissions") {
		t.Fatalf("a strict session was let off its permissions altogether: %v", asked)
	}
}

func TestTheStandardProfileNamesTheCommandsASessionMayNotRun(t *testing.T) {
	h := newHarness(t)

	h.replays(t, "clean.ndjson")
	h.drain(t, h.start(t, entity.ProfileStandard))

	asked := strings.Join(h.asked(t), " ")

	for _, refused := range []string{"Bash(rm:*)", "Bash(sudo:*)", "Bash(git push:*)"} {
		if !strings.Contains(asked, refused) {
			t.Fatalf("a standard session was not stopped from running %s: %s", refused, asked)
		}
	}
}

func TestASessionIsAskedForTheStreamNornCanReadAndOnlyTheMcpServersItWasGiven(t *testing.T) {
	h := newHarness(t)

	h.replays(t, "clean.ndjson")
	h.drain(t, h.start(t, entity.ProfileStandard))

	asked := h.asked(t)

	for _, wanted := range []string{
		"--print", "--output-format", "stream-json", "--verbose",
		"--strict-mcp-config", "--setting-sources", "project,local",
		"--add-dir", "--model", "opus", "do the work",
	} {
		if !slices.Contains(asked, wanted) {
			t.Fatalf("a session was started with %v, without %q", asked, wanted)
		}
	}
}

func TestASessionIsHandedNornsOwnToolsAndNothingElsesMcpConfig(t *testing.T) {
	h := newHarness(t)

	h.replays(t, "clean.ndjson")

	env := h.env(t, entity.ProfileStandard)

	session, err := h.driver.Start(t.Context(), env, entity.Task{Prompt: "do the work"})
	if err != nil {
		t.Fatalf("start the coding agent: %v", err)
	}

	h.drain(t, session)

	asked := h.asked(t)

	config := slices.Index(asked, "--mcp-config")
	if config < 0 || config+1 >= len(asked) || asked[config+1] != env.MCPConfig {
		t.Fatalf(
			"the session was started with %v, so it was never handed the tools norn wrote for "+
				"it. With --strict-mcp-config already on, that leaves the agent with no way to "+
				"start a service, ask a person or say it is done",
			asked,
		)
	}
}

func TestCarryingOnAskesForTheSameSessionRatherThanANewOne(t *testing.T) {
	h := newHarness(t)

	h.replays(t, "clean.ndjson")

	session, err := h.driver.Resume(
		t.Context(),
		h.env(t, entity.ProfileStandard),
		entity.DriverSession{ID: "session-01"},
		"carry on from where you left off",
	)
	if err != nil {
		t.Fatalf("carry on: %v", err)
	}

	h.drain(t, session)

	asked := h.asked(t)

	if !slices.Contains(asked, "--resume") || !slices.Contains(asked, "session-01") {
		t.Fatalf("carrying on asked for %v", asked)
	}

	if !slices.Contains(asked, "carry on from where you left off") {
		t.Fatalf("carrying on did not pass on what to do: %v", asked)
	}
}

func TestCarryingOnWithNoSessionToCarryOnFromIsRefusedByName(t *testing.T) {
	h := newHarness(t)

	_, err := h.driver.Resume(
		t.Context(), h.env(t, entity.ProfileStandard), entity.DriverSession{}, "carry on",
	)

	if !errors.Is(err, entity.ErrDriverSessionUnknown) {
		t.Fatalf("carrying on with nothing to carry on from answered %v", err)
	}
}

func TestAnInstalledAndSignedInAgentIsReportedReadyWithItsVersionAndAccount(t *testing.T) {
	h := newHarness(t)

	health := h.driver.Preflight(t.Context(), entity.DriverClaude)

	if !health.Ready() {
		t.Fatalf("an installed and signed-in agent reads %+v", health)
	}

	if health.Version != "2.1.239" {
		t.Fatalf("the agent's version came back as %q", health.Version)
	}

	if health.Account != "runner@example.test" {
		t.Fatalf("the account it is signed in as came back as %q", health.Account)
	}
}

func TestAnAgentThatIsNotSignedInIsAProblemWithTheMachineAndSaysHowToFixIt(t *testing.T) {
	h := newHarness(t)

	t.Setenv("NORN_TEST_AUTH", write(t, h.dir, "out.json", `{"loggedIn":false}`))

	health := h.driver.Preflight(t.Context(), entity.DriverClaude)

	if health.Ready() || !health.Installed {
		t.Fatalf("an agent that is installed but signed out reads %+v", health)
	}

	if !errors.Is(health.Fault(), entity.ErrDriverSignedOut) {
		t.Fatalf("an agent that is signed out is faulted as %v", health.Fault())
	}

	if !strings.Contains(health.Problem, "claude auth login") {
		t.Fatalf("nothing said how to sign the agent in: %q", health.Problem)
	}
}

func TestAnAgentThatIsNotOnThisMachineIsSaidToBeMissingRatherThanSignedOut(t *testing.T) {
	h := newHarness(t)

	if err := os.Remove(h.dir + "/claude"); err != nil {
		t.Fatalf("take the agent off this machine: %v", err)
	}

	health := h.driver.Preflight(t.Context(), entity.DriverClaude)

	if health.Installed {
		t.Fatalf("an agent that is not installed reads %+v", health)
	}

	if !errors.Is(health.Fault(), entity.ErrDriverMissing) {
		t.Fatalf("a missing agent is faulted as %v", health.Fault())
	}
}

func TestACodingAgentThisReleaseCannotDriveIsRefusedByName(t *testing.T) {
	h := newHarness(t)

	health := h.driver.Preflight(t.Context(), entity.DriverCodex)

	if health.Installed || health.Ready() {
		t.Fatalf("a coding agent this release cannot drive reads %+v", health)
	}

	if !strings.Contains(health.Problem, "claude code only") {
		t.Fatalf("nothing said which agents this release drives: %q", health.Problem)
	}
}

func TestWhatTheAgentPrintsOnStandardErrorIsKeptOutOfTheTranscript(t *testing.T) {
	h := newHarness(t)

	t.Setenv("NORN_TEST_STDERR", "warning: the wrapper had something to say")

	_, logs, _ := h.replay(t, "clean.ndjson")

	if len(logs) != 1 || !strings.Contains(logs[0], "the wrapper had something to say") {
		t.Fatalf("what the agent printed on standard error came back as %v", logs)
	}
}
