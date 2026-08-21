package servicemanager

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed systemd.service.tmpl
var systemdTemplate string

const (
	systemdUnit    = "norn-runner.service"
	systemdManager = "systemctl"
)

func plan(binary string, environment map[string]string) (Plan, error) {
	root, err := unitRoot()
	if err != nil {
		return Plan{}, err
	}

	content, err := render(systemdTemplate, struct {
		Binary      string
		Environment map[string]string
	}{Binary: binary, Environment: environment})
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Label:   systemdUnit,
		Path:    filepath.Join(root, systemdUnit),
		Content: content,
		Manager: systemdManager,
		Activate: []Command{
			{Args: []string{systemdManager, "--user", "daemon-reload"}},
			{Args: []string{systemdManager, "--user", "enable", "--now", systemdUnit}},
			{Args: []string{"loginctl", "enable-linger"}, Optional: true},
		},
		Remove: []Command{
			{Args: []string{systemdManager, "--user", "disable", "--now", systemdUnit}, Optional: true},
			{Args: []string{systemdManager, "--user", "daemon-reload"}, Optional: true},
		},
	}, nil
}

func unitRoot() (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "systemd", "user"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".config", "systemd", "user"), nil
}
