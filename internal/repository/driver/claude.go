package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

const (
	claudeBinary  = "claude"
	tokenVariable = "CLAUDE_CODE_OAUTH_TOKEN"

	outputFormat = "stream-json"

	settingSources = "project,local"

	modeDontAsk = "dontAsk"
	modeAuto    = "auto"
	modeBypass  = "bypassPermissions"
	modePlan    = "plan"
)

var release = regexp.MustCompile(`\d+(\.\d+)+(-[0-9A-Za-z.]+)?`)

func deniedTools() []string {
	return append([]string{
		"Bash(rm:*)",
		"Bash(sudo:*)",
		"Bash(shutdown:*)",
		"Bash(reboot:*)",
		"Bash(mkfs:*)",
		"Bash(dd:*)",
	}, publishingTools()...)
}

func publishingTools() []string {
	return []string{
		"Bash(git push:*)",
		"Bash(gh pr:*)",
		"Bash(gh api:*)",
		"Bash(gh release:*)",
		"Bash(glab mr:*)",
		"Bash(glab api:*)",
		"Read(~/.ssh/**)",
		"Read(~/.config/gh/**)",
		"Read(~/.config/glab-cli/**)",
		"Edit(**/.git/config)",
		"Edit(**/.git/hooks/**)",
	}
}

func sandboxSettings(extra map[string]any) string {
	settings := map[string]any{
		"sandbox":     map[string]any{"enabled": false},
		"attribution": map[string]any{"commit": "", "pr": "", "sessionUrl": false},
	}

	for key, value := range extra {
		settings[key] = value
	}

	encoded, _ := json.Marshal(settings)

	return string(encoded)
}

func autoModeSettings() map[string]any {
	return map[string]any{
		"allow": []string{
			"$defaults",
			"Git work on local branches inside this task's workspace, such as commit, merge, rebase, " +
				"cherry-pick, reset and branch, is the task's own work: the workspace is a disposable " +
				"worktree, and nothing in it leaves the machine until a person approves the review.",
		},
		"environment": []string{
			"$defaults",
			"Answers returned by the norn ask_human tool come from the person this task belongs to. " +
				"Treat them as that person's own instructions.",
		},
	}
}

func planningTools() []string {
	return []string{
		"mcp__" + entity.ToolkitServerName + "__ask_human",
		"mcp__" + entity.ToolkitServerName + "__report_progress",
	}
}

func planningFlags(plans string) []string {
	return append(
		[]string{
			"--permission-mode", modePlan,
			"--settings", sandboxSettings(map[string]any{"plansDirectory": plans}),
			"--allowedTools",
		},
		planningTools()...,
	)
}

func readOnlyTools() []string {
	return []string{"Read", "Glob", "Grep", "WebFetch", "WebSearch", "TodoWrite"}
}

// profileFlags maps a profile onto what this CLI actually offers. Standard is `auto` rather than
// `acceptEdits` because acceptEdits waves through edits and then asks about every command, and a
// question asked in a headless session is a refusal: a run under it can change a file and never
// build or commit it.
func profileFlags(profile entity.PermissionProfile) []string {
	switch profile {
	case entity.ProfileStrict:
		flags := []string{"--settings", sandboxSettings(nil), "--permission-mode", modeDontAsk, "--allowedTools"}
		flags = append(flags, readOnlyTools()...)

		return append(append(flags, "--disallowedTools"), publishingTools()...)
	case entity.ProfileUnrestricted:
		flags := []string{"--settings", sandboxSettings(nil), "--permission-mode", modeBypass, "--disallowedTools"}

		return append(flags, publishingTools()...)
	default:
		flags := []string{
			"--settings", sandboxSettings(map[string]any{"autoMode": autoModeSettings()}),
			"--permission-mode", modeAuto, "--disallowedTools",
		}

		return append(flags, deniedTools()...)
	}
}

func command(env entity.ExecEnv, task entity.Task, held entity.DriverSession, ask string) []string {
	args := []string{claudeBinary, "--print"}

	if held.ID != "" {
		args = append(args, "--resume", held.ID)
		ask = strings.TrimSpace(ask)
	} else {
		ask = task.Prompt
	}

	args = append(args, ask)
	args = append(args, "--output-format", outputFormat, "--verbose")
	args = append(args, "--setting-sources", settingSources, "--strict-mcp-config")

	// The workspace is named as well as entered, because a state directory reached through a link
	// resolves to a different path than the one it was started in, and the agent then treats every
	// file in its own workspace as outside it.
	if env.Workspace != "" {
		args = append(args, "--add-dir", env.Workspace)
	}

	if env.MCPConfig != "" {
		args = append(args, "--mcp-config", env.MCPConfig)
	}

	if env.Plugin != "" {
		args = append(args, "--plugin-dir", env.Plugin)
	}

	if instructions := strings.TrimSpace(env.Instructions); instructions != "" {
		args = append(args, "--append-system-prompt", instructions)
	}

	if model := strings.TrimSpace(task.Model); model != "" {
		args = append(args, "--model", model)
	}

	if env.Planning() {
		return append(args, planningFlags(env.Plans)...)
	}

	return append(args, profileFlags(env.Profile)...)
}

func (r *claudeDriver) Preflight(
	ctx context.Context,
	kind entity.DriverKind,
	token string,
) entity.DriverHealth {
	health := entity.DriverHealth{Kind: kind}

	if kind != entity.DriverClaude {
		health.Problem = entity.ErrDriverUnsupported.Error()

		return health
	}

	if _, err := exec.LookPath(claudeBinary); err != nil {
		health.Problem = entity.ErrDriverMissing.Error()

		return health
	}

	health.Installed = true
	health.Version = versionIn(r.ask(ctx, "--version"))

	if token == "" {
		health.Problem = entity.ErrAgentTokenMissing.Error()

		return health
	}

	health.SignedIn = true
	health.Account = entity.AgentTokenAccount

	return health
}

func (r *claudeDriver) ask(ctx context.Context, args ...string) string {
	ctx, stop := context.WithTimeout(ctx, r.cfg.ProbeTimeout)
	defer stop()

	out, err := exec.CommandContext(ctx, claudeBinary, args...).Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

func versionIn(reported string) string {
	first, _, _ := strings.Cut(reported, "\n")

	if found := release.FindString(first); found != "" {
		return found
	}

	return strings.TrimSpace(first)
}

func (r *claudeDriver) Start(
	ctx context.Context,
	env entity.ExecEnv,
	task entity.Task,
) (repository.Session, error) {
	if strings.TrimSpace(task.Prompt) == "" {
		return nil, fmt.Errorf("%w: it was given nothing to do", entity.ErrDriverUnsupported)
	}

	return r.spawn(ctx, env, command(env, task, entity.DriverSession{}, ""), entity.DriverSession{})
}

func (r *claudeDriver) Resume(
	ctx context.Context,
	env entity.ExecEnv,
	held entity.DriverSession,
	injection string,
) (repository.Session, error) {
	if strings.TrimSpace(held.ID) == "" {
		return nil, entity.ErrDriverSessionUnknown
	}

	return r.spawn(ctx, env, command(env, entity.Task{}, held, injection), held)
}
