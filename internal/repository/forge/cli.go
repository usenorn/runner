package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/proc"
	"github.com/usenorn/runner/internal/repository"
)

var address = regexp.MustCompile(`https://\S+`)

type cliForge struct {
	results config.Results
}

func New(results config.Results) repository.Forge {
	return &cliForge{results: results}
}

func (r *cliForge) Available(ctx context.Context, dir string) (entity.ForgeKind, bool) {
	for _, kind := range []entity.ForgeKind{entity.ForgeGitHub, entity.ForgeGitLab} {
		if _, err := exec.LookPath(string(kind)); err != nil {
			continue
		}

		if _, err := r.run(ctx, dir, kind, "auth", "status"); err == nil {
			return kind, true
		}
	}

	return "", false
}

func (r *cliForge) Existing(ctx context.Context, dir, branch string) (string, error) {
	kind, available := r.Available(ctx, dir)
	if !available {
		return "", fmt.Errorf("%w: %s", entity.ErrForgeAbsent, dir)
	}

	if kind == entity.ForgeGitLab {
		return r.existingRequest(ctx, dir, branch)
	}

	out, err := r.run(ctx, dir, kind, "pr", "view", branch, "--json", "url", "--jq", ".url")
	if err != nil {
		return "", nil
	}

	return address.FindString(out), nil
}

func (r *cliForge) existingRequest(ctx context.Context, dir, branch string) (string, error) {
	out, err := r.run(
		ctx, dir, entity.ForgeGitLab,
		"mr", "list", "--source-branch", branch, "--output", "json",
	)
	if err != nil {
		return "", nil
	}

	var open []struct {
		URL string `json:"web_url"`
	}

	if err := json.Unmarshal([]byte(out), &open); err != nil || len(open) == 0 {
		return "", nil
	}

	return open[0].URL, nil
}

func (r *cliForge) Open(
	ctx context.Context,
	dir string,
	request entity.PullRequest,
) (string, error) {
	kind, available := r.Available(ctx, dir)
	if !available {
		return "", fmt.Errorf("%w: %s", entity.ErrForgeAbsent, dir)
	}

	out, err := r.run(ctx, dir, kind, arguments(kind, request)...)
	if err == nil {
		return address.FindString(out), nil
	}

	if already := address.FindString(err.Error()); already != "" {
		return already, nil
	}

	return "", err
}

func arguments(kind entity.ForgeKind, request entity.PullRequest) []string {
	if kind == entity.ForgeGitLab {
		return []string{
			"mr", "create",
			"--title", request.Title,
			"--description", request.Body,
			"--source-branch", request.Branch,
			"--yes",
		}
	}

	return []string{
		"pr", "create",
		"--title", request.Title,
		"--body", request.Body,
		"--head", request.Branch,
	}
}

func (r *cliForge) run(
	ctx context.Context,
	dir string,
	kind entity.ForgeKind,
	args ...string,
) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.results.ForgeTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, string(kind), args...)
	command.Dir = dir
	command.Env = append(command.Environ(), "GIT_TERMINAL_PROMPT=0", "NO_COLOR=1")

	proc.Stoppable(command)

	var complaint bytes.Buffer

	command.Stderr = &complaint

	out, err := command.Output()
	if err != nil {
		return "", fmt.Errorf(
			"%s %s: %w%s", kind, strings.Join(args, " "), err, said(complaint.String()),
		)
	}

	return strings.TrimSpace(string(out)), nil
}

func said(complaint string) string {
	trimmed := strings.TrimSpace(complaint)
	if trimmed == "" {
		return ""
	}

	return " — " + strings.ReplaceAll(trimmed, "\n", "; ")
}
