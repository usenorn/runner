package driver_test

import (
	"errors"
	"os"
	"path/filepath"
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

func TestASessionCarriesNornsInstructionsAndSkillsBesideTheRepositorysOwn(t *testing.T) {
	h := newHarness(t)

	h.replays(t, "clean.ndjson")

	env := h.env(t, entity.ProfileStandard)
	env.Instructions = "Commit small, and write no comments."
	env.Plugin = t.TempDir()

	session, err := h.driver.Start(t.Context(), env, entity.Task{Prompt: "do the work"})
	if err != nil {
		t.Fatalf("start the coding agent: %v", err)
	}

	h.drain(t, session)

	asked := h.asked(t)

	appended := slices.Index(asked, "--append-system-prompt")
	if appended < 0 || asked[appended+1] != env.Instructions {
		t.Fatalf(
			"the session was started with %v; norn's instructions are appended so the "+
				"repository's own CLAUDE.md and AGENTS.md still load and still decide how it commits",
			asked,
		)
	}

	if slices.Contains(asked, "--system-prompt") {
		t.Fatalf("the session replaced its system prompt rather than adding to it: %v", asked)
	}

	plugin := slices.Index(asked, "--plugin-dir")
	if plugin < 0 || asked[plugin+1] != env.Plugin {
		t.Fatalf("the session was started with %v, without the skills norn gave the agent", asked)
	}

	if !slices.Contains(asked, "project,local") {
		t.Fatalf("the session stopped reading the repository's own settings: %v", asked)
	}
}

func TestASessionWithNothingFromNornAsksForNoMore(t *testing.T) {
	h := newHarness(t)

	h.replays(t, "clean.ndjson")
	h.drain(t, h.start(t, entity.ProfileStandard))

	asked := h.asked(t)

	for _, unwanted := range []string{"--append-system-prompt", "--plugin-dir"} {
		if slices.Contains(asked, unwanted) {
			t.Fatalf("a session with no instructions or skills was started with %v", asked)
		}
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

func TestAnInstalledAgentWithATokenIsReportedReadyWithItsVersion(t *testing.T) {
	h := newHarness(t)

	health := h.driver.Preflight(t.Context(), entity.DriverClaude, "sk-ant-oat01-test")

	if !health.Ready() {
		t.Fatalf("an installed agent with a token reads %+v", health)
	}

	if health.Version != "2.1.239" {
		t.Fatalf("the agent's version came back as %q", health.Version)
	}
}

func TestAnAgentWithNoTokenIsAProblemWithTheMachineAndSaysHowToFixIt(t *testing.T) {
	h := newHarness(t)

	health := h.driver.Preflight(t.Context(), entity.DriverClaude, "")

	if health.Ready() || !health.Installed {
		t.Fatalf("an agent that is installed but has no token reads %+v", health)
	}

	if !errors.Is(health.Fault(), entity.ErrAgentTokenMissing) {
		t.Fatalf("an agent with no token is faulted as %v", health.Fault())
	}

	if !strings.Contains(health.Problem, "claude setup-token") {
		t.Fatalf("nothing said how to give the agent a token: %q", health.Problem)
	}
}

func TestTheAgentIsHandedTheTokenInItsEnvironmentAndNowhereElse(t *testing.T) {
	h := newHarness(t)

	seen := filepath.Join(h.dir, "env")
	t.Setenv("NORN_TEST_ENV", seen)

	env := h.env(t, entity.ProfileStandard)
	env.AgentToken = "sk-ant-oat01-handed"

	session, err := h.driver.Start(t.Context(), env, entity.Task{Prompt: "do the work"})
	if err != nil {
		t.Fatalf("start the coding agent: %v", err)
	}

	h.drain(t, session)

	body, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("read what the agent was started with: %v", err)
	}

	if !strings.Contains(string(body), "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-handed") {
		t.Fatalf("the agent was not started with its token:\n%s", body)
	}

	if slices.ContainsFunc(h.asked(t), func(arg string) bool { return strings.Contains(arg, "sk-ant-oat01") }) {
		t.Fatal("the token was passed on the command line, where anybody listing processes can read it")
	}
}

func TestAnAgentThatIsNotOnThisMachineIsSaidToBeMissingRatherThanSignedOut(t *testing.T) {
	h := newHarness(t)

	if err := os.Remove(h.dir + "/claude"); err != nil {
		t.Fatalf("take the agent off this machine: %v", err)
	}

	health := h.driver.Preflight(t.Context(), entity.DriverClaude, "sk-ant-oat01-test")

	if health.Installed {
		t.Fatalf("an agent that is not installed reads %+v", health)
	}

	if !errors.Is(health.Fault(), entity.ErrDriverMissing) {
		t.Fatalf("a missing agent is faulted as %v", health.Fault())
	}
}

func TestACodingAgentThisReleaseCannotDriveIsRefusedByName(t *testing.T) {
	h := newHarness(t)

	health := h.driver.Preflight(t.Context(), entity.DriverCodex, "sk-ant-oat01-test")

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
