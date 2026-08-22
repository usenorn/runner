package gitcmd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/usenorn/runner/internal/pkg/proc"
)

const Binary = "git"

var environment = []string{
	"GIT_OPTIONAL_LOCKS=0",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_ASKPASS=",
	"GCM_INTERACTIVE=never",
}

func Installed() bool {
	_, err := exec.LookPath(Binary)

	return err == nil
}

func Command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}

	command := exec.CommandContext(ctx, Binary, args...)
	command.Env = append(command.Environ(), environment...)

	proc.Stoppable(command)

	return command
}

func Run(ctx context.Context, dir string, args ...string) (string, error) {
	command := Command(ctx, dir, args...)

	var complaint bytes.Buffer

	command.Stderr = &complaint

	out, err := command.Output()
	if err != nil {
		return "", fmt.Errorf(
			"git %s: %w%s", strings.Join(args, " "), err, said(complaint.String()),
		)
	}

	return strings.TrimRight(string(out), "\n"), nil
}

func Lines(ctx context.Context, dir string, args ...string) ([]string, error) {
	out, err := Run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}

	if out == "" {
		return nil, nil
	}

	return strings.Split(out, "\n"), nil
}

func said(complaint string) string {
	trimmed := strings.TrimSpace(complaint)
	if trimmed == "" {
		return ""
	}

	return " — " + strings.ReplaceAll(trimmed, "\n", "; ")
}
