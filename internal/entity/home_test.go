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

	environment := entity.TaskEnvironment(entity.RuntimeProcess, host, "/Users/vlad", home, func(string) bool { return false })

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

	environment := entity.TaskEnvironment(entity.RuntimeProcess, host, "/Users/vlad", home, func(path string) bool {
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

func TestATasksGitConfigSignsAsThePersonAndReachesNothingElseOfTheirs(t *testing.T) {
	config := entity.TaskGitConfig(entity.GitIdentity{Name: `Rae "R" Okafor`, Email: "rae@example.com"})

	for _, want := range []string{
		"\tname = \"Rae \\\"R\\\" Okafor\"\n",
		"\temail = \"rae@example.com\"\n",
		"[credential]\n\thelper =\n",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("the task's git config does not hold %q:\n%s", want, config)
		}
	}

	if strings.Contains(config, "[include]") {
		t.Fatalf(
			"the task's git config still includes the person's own, which carries their helpers "+
				"and insteadOf rewrites and is out of the sandbox's reach:\n%s",
			config,
		)
	}
}

func TestATaskInAContainerGetsItsOwnHomeAndNothingFromTheHost(t *testing.T) {
	home := entity.RunHomeOf("/state/runs/exec-01ABC")
	host := []string{"PATH=/opt/homebrew/bin:/usr/bin", "LANG=en_GB.UTF-8", "GOPATH=/Users/vlad/go"}

	environment := entity.TaskEnvironment(entity.RuntimeDocker, host, "/Users/vlad", home, func(string) bool { return true })

	for _, name := range []string{"PATH", "LANG", "GOPATH", "CARGO_HOME"} {
		if value, found := lookup(environment, name); found {
			t.Fatalf(
				"%s=%q reached a task in a container. The host's paths mean nothing inside the "+
					"image, and a macOS PATH there finds no shell at all",
				name, value,
			)
		}
	}

	if got, _ := lookup(environment, "HOME"); got != home.Root {
		t.Fatalf("HOME in the container is %q, want the run's own %q", got, home.Root)
	}

	if got, _ := lookup(environment, "HOST"); got != "0.0.0.0" {
		t.Fatalf(
			"HOST in the container is %q. A service listening on the container's own loopback "+
				"is one no published port can reach",
			got,
		)
	}
}
