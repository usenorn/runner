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
	sections := []string{
		heading(execution),
		briefing(execution, snapshot, plan),
		standingRules(),
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
		"- Do not push, open a pull request, or touch any remote. That is done for you afterwards.",
		"- The project's own instruction files are in the workspace and apply to you.",
		"- When a decision is not yours to make, ask a person rather than guessing: " +
			"`norn ask \"your question\" --option \"one answer\" --option \"another\"`. It waits " +
			"a little for a reply; if nobody has answered by then it tells you to stop, and norn " +
			"starts you again with the answer once somebody gives one. Add " +
			"`--meanwhile \"what you will do\"` instead when you do not need to stop.",
		"- When the work is done, say what you changed and why, in a few sentences.",
	}, "\n")
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
