package entity_test

import (
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func delegated() entity.Execution {
	return entity.Execution{
		ID:          "exec-01ABC",
		IssueKey:    "NORN-51",
		Attempt:     1,
		Title:       "Drive a coding agent",
		Description: "Spawn the CLI and read what it says.",
		Brief:       "This agent works on the runner.",
		Model:       "opus",
	}
}

func copied() entity.Snapshot {
	return entity.Snapshot{
		Workspace: "/home/runner/.norn/runs/exec-01ABC/workspace",
		Repositories: []entity.SnapshotRepository{
			{
				Name:    "runner",
				RelPath: "runner",
				Branch:  "norn/NORN-51/runner",
				BaseSHA: "0123456789abcdef0123456789abcdef01234567",
			},
			{Name: "web", RelPath: "web", Branch: "norn/NORN-51/web", BaseSHA: "abcdef0123456"},
		},
	}
}

func TestTheTaskNamesTheIssueTheWorkspaceAndEveryBranchTheAgentIsOn(t *testing.T) {
	task := entity.ComposeTask(delegated(), copied(), entity.RunPlan{Source: entity.PlanNone})

	for _, wanted := range []string{
		"NORN-51", "Drive a coding agent", "Spawn the CLI and read what it says.",
		"This agent works on the runner.",
		"/home/runner/.norn/runs/exec-01ABC/workspace",
		"runner", "norn/NORN-51/runner", "web", "norn/NORN-51/web",
		"0123456789ab",
	} {
		if !strings.Contains(task.Prompt, wanted) {
			t.Fatalf("the task the agent was given never mentions %q:\n%s", wanted, task.Prompt)
		}
	}

	if task.Model != "opus" {
		t.Fatalf("the model the delegation asked for came through as %q", task.Model)
	}
}

func TestTheTaskTellsTheAgentToStayPutCommitAndLeaveTheRemoteAlone(t *testing.T) {
	task := entity.ComposeTask(delegated(), copied(), entity.RunPlan{Source: entity.PlanNone})

	for _, wanted := range []string{
		"only inside this workspace", "Commit your work", "Do not push",
	} {
		if !strings.Contains(task.Prompt, wanted) {
			t.Fatalf("the standing rules never say %q:\n%s", wanted, task.Prompt)
		}
	}
}

func TestTheTaskTellsTheAgentHowToAskAPersonRatherThanGuess(t *testing.T) {
	task := entity.ComposeTask(delegated(), copied(), entity.RunPlan{Source: entity.PlanNone})

	for _, wanted := range []string{"norn ask", "--option", "--meanwhile"} {
		if !strings.Contains(task.Prompt, wanted) {
			t.Fatalf(
				"the standing rules never say %q, so an agent that needs a decision has no way "+
					"to know it can ask for one and will guess:\n%s",
				wanted, task.Prompt,
			)
		}
	}
}

func TestARunPlanIsPointedAtOnlyWhenTheCodebaseHasOne(t *testing.T) {
	without := entity.ComposeTask(delegated(), copied(), entity.RunPlan{
		Source: entity.PlanNone,
	}).Prompt

	if strings.Contains(without, "run plan") {
		t.Fatalf("a codebase with no run plan was told to follow one:\n%s", without)
	}

	with := entity.ComposeTask(delegated(), copied(), entity.RunPlan{
		Source: entity.PlanCodebase,
		Path:   ".norn/run-plan.yaml",
	}).Prompt

	if !strings.Contains(with, ".norn/run-plan.yaml") {
		t.Fatalf("a codebase with a run plan was not told where it is:\n%s", with)
	}
}

func TestAnAttemptAfterTheFirstSaysItIsCarryingOnEarlierBranches(t *testing.T) {
	again := delegated()
	again.Attempt = 3

	task := entity.ComposeTask(again, copied(), entity.RunPlan{Source: entity.PlanNone})

	if !strings.Contains(task.Prompt, "attempt 3") {
		t.Fatalf("a later attempt was not told it is one:\n%s", task.Prompt)
	}
}

func TestAVeryLongDescriptionIsCutRatherThanSentWholeToTheCommandLine(t *testing.T) {
	long := delegated()
	long.Description = strings.Repeat("a very long description. ", entity.PromptMaxBytes/10)

	task := entity.ComposeTask(long, copied(), entity.RunPlan{Source: entity.PlanNone})

	if !strings.Contains(task.Prompt, entity.PromptTruncatedAt) {
		t.Fatalf("a description longer than norn sends was passed on whole")
	}

	if len(task.Prompt) > entity.PromptMaxBytes*2 {
		t.Fatalf("the task came to %d bytes", len(task.Prompt))
	}
}
