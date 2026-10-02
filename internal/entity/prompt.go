package entity

import (
	"fmt"
	"strings"
)

const (
	PromptMaxBytes    = 64 << 10
	PromptTruncatedAt = "\n\n[the rest of this description was left out because it is very long]"
)

func ComposeTask(execution Execution, snapshot Snapshot, plan RunPlan) Task {
	rules := standingRules()
	if execution.Planning() {
		rules = planningRules()
	}

	sections := []string{
		heading(execution),
		briefing(execution, snapshot, plan),
		rules,
	}

	return Task{
		Prompt: strings.Join(compact(sections), "\n\n"),
		Model:  execution.Model,
	}
}

func heading(execution Execution) string {
	title := strings.TrimSpace(execution.Title)
	if title == "" {
		title = "an issue with no title"
	}

	parts := []string{fmt.Sprintf("# %s %s", execution.IssueKey, title)}

	if description := fit(execution.Description, PromptMaxBytes); description != "" {
		parts = append(parts, description)
	}

	if brief := strings.TrimSpace(execution.Brief); brief != "" {
		parts = append(parts, "## What this agent is for\n\n"+brief)
	}

	return strings.Join(parts, "\n\n")
}

func briefing(execution Execution, snapshot Snapshot, plan RunPlan) string {
	lines := []string{
		"## Where you are working",
		"",
		fmt.Sprintf(
			"You are in a copy of the codebase made for this run alone, at %s. Nothing outside "+
				"it belongs to this run.",
			snapshot.Workspace,
		),
	}

	if len(snapshot.Repositories) > 0 {
		lines = append(lines, "", "It holds:")

		for _, repository := range snapshot.Repositories {
			lines = append(lines, fmt.Sprintf(
				"- %s at %s, on the branch %s, cut from %s",
				repository.Name, repository.RelPath, repository.Branch,
				ShortSHA(repository.BaseSHA),
			))
		}
	}

	if len(snapshot.Shared) > 0 {
		lines = append(lines, "", fmt.Sprintf(
			"Alongside them are %d file(s) that sit outside any repository and were copied in "+
				"with it.", len(snapshot.Shared),
		))
	}

	if plan.Source == PlanCodebase && plan.Path != "" {
		lines = append(lines, "", "This codebase has a run plan at "+plan.Path+". Follow it.")
	}

	if execution.Attempt > 1 {
		lines = append(lines, "", fmt.Sprintf(
			"This is attempt %d at this issue, carrying on the branches an earlier attempt made.",
			execution.Attempt,
		))
	}

	return strings.Join(lines, "\n")
}

func standingRules() string {
	return strings.Join([]string{
		"## How to work here",
		"",
		"- Work only inside this workspace. Do not read or write anything outside it.",
		"- Commit your work in each repository you changed, following that project's own commit " +
			"convention. Uncommitted work is work nobody will see.",
		"- Do not fetch, pull, push or open a pull request: this workspace cannot reach any " +
			"remote, and publishing is done for you afterwards. Every repository's origin/<default " +
			"branch> was fetched when this workspace was made and again each time norn hands the " +
			"run back to you. When you need the latest — to merge or rebase onto it, or to check " +
			"for conflicts — call `refresh_remote`, then merge or rebase locally.",
		"- The project's own instruction files are in the workspace and apply to you.",
		"- Norn's own tools are how you touch anything outside your editing, and they are " +
			"named `mcp__norn__*`. Read what each one says before you reach for a shell.",
		"- Start every long-running process with `start_service`, never yourself in the " +
			"background. Norn keeps it in a process group of its own, captures its output and " +
			"stops it when this run ends; a process you daemonise outlives the run and is " +
			"nobody's to clean up. Run one-off commands — install, build, migrate, test — with " +
			"`run_step` so they are timed and recorded.",
		"- Never choose a port. Write `${ports.web}` in a service's command, environment or " +
			"health check and norn substitutes a free one, which the process also reads as " +
			"`NORN_PORT_WEB`.",
		"- Open something for a person to look at with `expose_preview`, on a service norn is " +
			"already running.",
		"- When a decision is not yours to make, `ask_human` rather than guessing, and offer " +
			"the answers you would accept. It waits a little for a reply; if nobody has " +
			"answered by then it tells you to stop, and norn starts you again with the answer " +
			"once somebody gives one. Set `blocking` to false and say what you will do " +
			"meanwhile when you do not need to stop.",
		"- Say where you are with `report_progress` when you move between real phases of the " +
			"work, and hand over a file worth keeping with `publish_artifact`.",
		"- When the work is committed, call `complete_task` once with what you changed and why, " +
			"and end your turn without saying anything else.",
		"- Working through bash instead? The same things are `norn service …`, `norn preview " +
			"…` and `norn ask \"your question\" --option \"one answer\"`.",
	}, "\n")
}

func planningRules() string {
	return strings.Join([]string{
		"## How to plan here",
		"",
		"- You are planning, not implementing. Read the code, work out what has to change, and " +
			"write a plan. Change nothing in the workspace: a person approves the plan before " +
			"any of it is built, and you are started again to build it once they have.",
		"- When a decision is not yours to make, `ask_human` with `blocking` set, and offer the " +
			"answers you would accept. Ask before you settle the plan, not inside it. If nobody " +
			"answers in time you are told to stop; norn starts you again with the answer.",
		"- Write the plan to your plan file: what you will change and where, in the order you " +
			"will do it, how you will check it works, and what you are unsure of. Write it for " +
			"a reviewer who has not read the code.",
		"- Say where you are with `report_progress` as you go.",
		"- End your turn once the plan is written. Do not call `complete_task` while planning.",
	}, "\n")
}

func PlanProposed() string {
	return "the coding agent proposed a plan and is waiting for somebody to approve it before " +
		"it changes anything"
}

func PlanApprovedInjection(plan string) string {
	return "Your plan was approved as it stands. Build it now, following it step by step:\n\n" +
		strings.TrimSpace(plan) + "\n\n" + standingRules()
}

func PlanRevisionInjection(feedback string) string {
	return "A person read your plan and wants it changed before approving it:\n\n" +
		strings.TrimSpace(feedback) + "\n\n" +
		"Revise the plan to answer this and write the whole new version to your plan file. " +
		"You are still planning: change nothing in the workspace."
}

func compact(values []string) []string {
	kept := make([]string, 0, len(values))

	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}

	return kept
}

func fit(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= limit {
		return trimmed
	}

	return strings.TrimSpace(trimmed[:limit]) + PromptTruncatedAt
}
