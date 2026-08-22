package scanner

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/usenorn/runner/internal/entity"
)

const originHead = "refs/remotes/origin/HEAD"

var gitEnvironment = []string{
	"GIT_OPTIONAL_LOCKS=0",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_ASKPASS=",
	"GCM_INTERACTIVE=never",
}

func (r *filesystemScanner) probe(ctx context.Context, dir string) (entity.GitFacts, bool) {
	answers, ok := r.git(ctx, dir,
		"rev-parse",
		"--absolute-git-dir",
		"--git-common-dir",
		"--is-bare-repository",
		"--is-inside-work-tree",
	)
	if !ok || len(answers) != 4 {
		return entity.GitFacts{}, false
	}

	facts := entity.GitFacts{
		Dir:            dir,
		GitDir:         absolute(dir, answers[0]),
		CommonDir:      absolute(dir, answers[1]),
		Bare:           answers[2] == "true",
		InsideWorkTree: answers[3] == "true",
	}

	if facts.Bare || !facts.InsideWorkTree {
		return facts, true
	}

	if toplevel, ok := r.one(ctx, dir, "rev-parse", "--show-toplevel"); ok {
		facts.TopLevel = absolute(dir, toplevel)
	}

	if superproject, ok := r.one(ctx, dir, "rev-parse", "--show-superproject-working-tree"); ok {
		facts.Superproject = absolute(dir, superproject)
	}

	facts.RemoteURL, _ = r.one(ctx, dir, "config", "--get", "remote.origin.url")
	facts.DefaultBranch = r.defaultBranch(ctx, dir)

	return facts, true
}

func (r *filesystemScanner) defaultBranch(ctx context.Context, dir string) string {
	if head, ok := r.one(ctx, dir, "symbolic-ref", "--short", originHead); ok {
		if _, branch, found := strings.Cut(head, "/"); found {
			return branch
		}

		return head
	}

	head, ok := r.one(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if !ok || head == "HEAD" {
		return ""
	}

	return head
}

func (r *filesystemScanner) one(ctx context.Context, dir string, args ...string) (string, bool) {
	answers, ok := r.git(ctx, dir, args...)
	if !ok || len(answers) != 1 {
		return "", false
	}

	return answers[0], true
}

func (r *filesystemScanner) git(ctx context.Context, dir string, args ...string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.ProbeTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	command.Env = append(command.Environ(), gitEnvironment...)

	out, err := command.Output()
	if err != nil {
		return nil, false
	}

	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil, false
	}

	return strings.Split(trimmed, "\n"), true
}

func absolute(dir, answer string) string {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return ""
	}

	joined := trimmed
	if !filepath.IsAbs(trimmed) {
		joined = filepath.Join(dir, trimmed)
	}

	resolved, err := resolve(joined)
	if err != nil {
		return filepath.Clean(joined)
	}

	return resolved
}
