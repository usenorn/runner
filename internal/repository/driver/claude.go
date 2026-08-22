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
	claudeBinary = "claude"

	outputFormat = "stream-json"

	settingSources = "project,local"

	modeDontAsk = "dontAsk"
	modeAuto    = "auto"
	modeBypass  = "bypassPermissions"
)

var release = regexp.MustCompile(`\d+(\.\d+)+(-[0-9A-Za-z.]+)?`)

func deniedTools() []string {
	return []string{
		"Bash(rm:*)",
		"Bash(sudo:*)",
		"Bash(shutdown:*)",
		"Bash(reboot:*)",
		"Bash(mkfs:*)",
		"Bash(dd:*)",
		"Bash(git push:*)",
		"Bash(gh pr create:*)",
		"Bash(gh release:*)",
	}
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
		return append(
			[]string{"--permission-mode", modeDontAsk, "--allowedTools"}, readOnlyTools()...,
		)
	case entity.ProfileUnrestricted:
		return []string{"--permission-mode", modeBypass}
	case entity.ProfileStandard:
		return append([]string{"--permission-mode", modeAuto, "--disallowedTools"}, deniedTools()...)
	default:
		return append([]string{"--permission-mode", modeAuto, "--disallowedTools"}, deniedTools()...)
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

	if model := strings.TrimSpace(task.Model); model != "" {
		args = append(args, "--model", model)
	}

	return append(args, profileFlags(env.Profile)...)
}

type account struct {
	LoggedIn bool   `json:"loggedIn"`
	Method   string `json:"authMethod"`
	Email    string `json:"email"`
}

func (r *claudeDriver) Preflight(ctx context.Context, kind entity.DriverKind) entity.DriverHealth {
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

	var signed account

	if err := json.Unmarshal([]byte(r.ask(ctx, "auth", "status", "--json")), &signed); err != nil {
		health.Problem = entity.ErrDriverSignedOut.Error()

		return health
	}

	if !signed.LoggedIn {
		health.Problem = entity.ErrDriverSignedOut.Error()

		return health
	}

	health.SignedIn = true
	health.Account = strings.TrimSpace(signed.Email)

	if health.Account == "" {
		health.Account = strings.TrimSpace(signed.Method)
	}

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
