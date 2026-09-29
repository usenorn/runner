package run_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
	runrepo "github.com/usenorn/runner/internal/repository/run"
)

func TestARunsGitSignsCommitsAsThePersonButCannotReachTheirCredentials(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed, so there is no config to read back")
	}

	host := t.TempDir()
	global := "[user]\n\tname = Vlad\n\temail = vlad@example.test\n[credential]\n\thelper = store\n"

	if err := os.WriteFile(filepath.Join(host, ".gitconfig"), []byte(global), 0o600); err != nil {
		t.Fatalf("write the person's git config: %v", err)
	}

	t.Setenv("HOME", host)
	t.Setenv("XDG_CONFIG_HOME", "")

	dir, ctx := store(t)

	path, err := runrepo.New(dir).Open(ctx, "exec-01HOM")
	if err != nil {
		t.Fatalf("open a run: %v", err)
	}

	home := entity.RunHomeOf(path)
	read := func(args ...string) string {
		command := exec.Command("git", append([]string{"config"}, args...)...)
		command.Env = entity.TaskEnvironment(os.Environ(), host, home, func(string) bool { return false })
		command.Dir = home.Root

		out, _ := command.Output()

		return strings.TrimSpace(string(out))
	}

	if name := read("--get", "user.name"); name != "Vlad" {
		t.Fatalf(
			"a run's git signs commits as %q. The agent commits on the person's behalf, and a "+
				"commit with nobody's name on it is refused outright",
			name,
		)
	}

	if helper := read("--get", "credential.helper"); helper != "" {
		t.Fatalf(
			"a run's git still reaches the credential helper %q, so the agent could push as "+
				"the person although only this machine is meant to",
			helper,
		)
	}

	if _, err := os.Stat(home.Tmp); err != nil {
		t.Fatalf("the run has no temporary folder of its own: %v", err)
	}
}
