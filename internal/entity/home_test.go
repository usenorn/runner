package entity_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func lookup(environment []string, name string) (string, bool) {
	for _, entry := range slices.Backward(environment) {
		if key, value, _ := strings.Cut(entry, "="); key == name {
			return value, true
		}
	}

	return "", false
}

func TestATaskLivesInAHomeOfItsOwnWithNoneOfTheMachinesCredentialsAround(t *testing.T) {
	home := entity.RunHomeOf("/state/runs/exec-01ABC")
	host := []string{
		"PATH=/usr/bin",
		"HOME=/Users/vlad",
		"TMPDIR=/var/folders/tmp",
		"XDG_CONFIG_HOME=/Users/vlad/.config",
		"SSH_AUTH_SOCK=/private/tmp/agent.sock",
		"GH_TOKEN=gho_secret",
		"GITHUB_TOKEN=ghp_secret",
		"ANTHROPIC_API_KEY=sk-ant-api",
		"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-machine",
		"GIT_ASKPASS=/usr/local/bin/askpass",
		"GIT_CONFIG_GLOBAL=/Users/vlad/.gitconfig",
		"NORN_TOKEN=nrn_connect",
		"LANG=en_GB.UTF-8",
	}

	environment := entity.TaskEnvironment(host, "/Users/vlad", home, func(string) bool { return false })

	for name, want := range map[string]string{
		"HOME":              "/state/runs/exec-01ABC/home",
		"TMPDIR":            "/state/runs/exec-01ABC/tmp",
		"XDG_CONFIG_HOME":   "/state/runs/exec-01ABC/home/.config",
		"CLAUDE_CONFIG_DIR": "/state/runs/exec-01ABC/home/.claude",
		"PATH":              "/usr/bin",
		"LANG":              "en_GB.UTF-8",
	} {
		if got, _ := lookup(environment, name); got != want {
			t.Errorf("%s is %q, want %q", name, got, want)
		}
	}

	for _, name := range []string{
		"SSH_AUTH_SOCK", "GH_TOKEN", "GITHUB_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN",
		"GIT_ASKPASS", "GIT_CONFIG_GLOBAL", "NORN_TOKEN",
	} {
		if value, found := lookup(environment, name); found {
			t.Errorf(
				"%s=%q reached the task. Anything the machine is signed in with lets one run act "+
					"as the person, and lets two runs on this machine trip over each other's session",
				name, value,
			)
		}
	}
}

func TestATaskStillFindsTheToolchainsInstalledUnderThePersonsHome(t *testing.T) {
	home := entity.RunHomeOf("/state/runs/exec-01ABC")
	installed := map[string]bool{
		filepath.Join("/Users/vlad", ".cargo"):  true,
		filepath.Join("/Users/vlad", ".rustup"): true,
		filepath.Join("/Users/vlad", "go"):      true,
	}
	host := []string{"PATH=/usr/bin", "GOPATH=/opt/go"}

	environment := entity.TaskEnvironment(host, "/Users/vlad", home, func(path string) bool {
		return installed[path]
	})

	if got, _ := lookup(environment, "CARGO_HOME"); got != "/Users/vlad/.cargo" {
		t.Fatalf(
			"CARGO_HOME is %q. A toolchain installed under the person's home is found through "+
				"HOME, and a task with a home of its own would otherwise lose it",
			got,
		)
	}

	if got, _ := lookup(environment, "GOPATH"); got != "/opt/go" {
		t.Fatalf("GOPATH is %q; a toolchain the machine already names is left where it points", got)
	}

	if _, found := lookup(environment, "NVM_DIR"); found {
		t.Fatal("NVM_DIR was set although nothing is installed there")
	}
}

func TestATasksGitConfigKeepsThePersonsSettingsButNoneOfTheirCredentialHelpers(t *testing.T) {
	config := entity.TaskGitConfig(entity.HostGitConfigs("/Users/vlad", ""))

	for _, want := range []string{
		"path = /Users/vlad/.config/git/config",
		"path = /Users/vlad/.gitconfig",
		"[credential]\n\thelper =\n",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("the task's git config does not hold %q:\n%s", want, config)
		}
	}

	if strings.Index(config, "[credential]") < strings.Index(config, "[include]") {
		t.Fatalf("the credential reset comes before the includes it has to override:\n%s", config)
	}
}
