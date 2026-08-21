package servicemanager

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

//go:embed launchd.plist.tmpl
var launchdTemplate string

const (
	launchdLabel   = "site.norn.runner"
	launchdManager = "launchctl"
)

func plan(binary string, environment map[string]string) (Plan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Plan{}, fmt.Errorf("resolve home directory: %w", err)
	}

	content, err := render(launchdTemplate, struct {
		Label       string
		Binary      string
		Environment map[string]string
	}{Label: launchdLabel, Binary: binary, Environment: environment})
	if err != nil {
		return Plan{}, err
	}

	target := "gui/" + strconv.Itoa(os.Getuid())
	service := target + "/" + launchdLabel
	path := filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")

	return Plan{
		Label:   launchdLabel,
		Path:    path,
		Content: content,
		Manager: launchdManager,
		Activate: []Command{
			{Args: []string{launchdManager, "bootout", service}, Optional: true},
			{Args: []string{launchdManager, "bootstrap", target, path}},
			{Args: []string{launchdManager, "enable", service}},
			{Args: []string{launchdManager, "kickstart", "-k", service}},
		},
		Remove: []Command{
			{Args: []string{launchdManager, "bootout", service}, Optional: true},
		},
	}, nil
}
