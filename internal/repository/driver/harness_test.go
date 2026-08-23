package driver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	driverrepo "github.com/usenorn/runner/internal/repository/driver"
	processrepo "github.com/usenorn/runner/internal/repository/process"
)

const fake = `#!/bin/sh
printf '%s\n' "$@" >> "$NORN_TEST_ARGV"
if [ "$1" = "--version" ]; then echo "2.1.239 (Claude Code)"; exit 0; fi
if [ "$1" = "auth" ]; then cat "$NORN_TEST_AUTH"; exit 0; fi
if [ -n "$NORN_TEST_STDERR" ]; then printf '%s\n' "$NORN_TEST_STDERR" >&2; fi
cat "$NORN_TEST_STREAM"
exit "${NORN_TEST_EXIT:-0}"
`

type harness struct {
	driver repository.Driver
	dir    string
	argv   string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("there is no shell here, so no coding agent can be stood in for")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "claude")

	if err := os.WriteFile(path, []byte(fake), 0o700); err != nil {
		t.Fatalf("stand in for the coding agent: %v", err)
	}

	argv := filepath.Join(dir, "argv")

	// The stand-in is the only coding agent on the path, so a machine that really has one
	// installed cannot answer a test that is about not having one.
	t.Setenv("PATH", strings.Join([]string{dir, "/bin", "/usr/bin"}, string(os.PathListSeparator)))
	t.Setenv("NORN_TEST_ARGV", argv)
	t.Setenv("NORN_TEST_AUTH", write(t, dir, "auth.json", `{"loggedIn":true,"authMethod":"claude.ai","email":"runner@example.test"}`))
	t.Setenv("NORN_TEST_STREAM", os.DevNull)

	return &harness{
		driver: driverrepo.New(processrepo.New(), settings()),
		dir:    dir,
		argv:   argv,
	}
}

func settings() config.Driver {
	return config.Driver{
		Profile:        config.ProfileStandard,
		ProbeTimeout:   10 * time.Second,
		SessionTimeout: time.Minute,
		StopGrace:      time.Second,
		ResumeAttempts: 1,
	}
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

func (h *harness) replays(t *testing.T, fixture string) {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("find %s: %v", fixture, err)
	}

	t.Setenv("NORN_TEST_STREAM", path)
}

func (h *harness) start(t *testing.T, profile entity.PermissionProfile) repository.Session {
	t.Helper()

	session, err := h.driver.Start(t.Context(), h.env(t, profile), entity.Task{
		Prompt: "do the work",
		Model:  "opus",
	})
	if err != nil {
		t.Fatalf("start the coding agent: %v", err)
	}

	return session
}

func (h *harness) env(t *testing.T, profile entity.PermissionProfile) entity.ExecEnv {
	t.Helper()

	return entity.ExecEnv{
		ExecutionID: "exec-01ABC",
		Workspace:   t.TempDir(),
		Environment: os.Environ(),
		MCPConfig:   filepath.Join(t.TempDir(), entity.RunMCPFile),
		Profile:     profile,
	}
}

func (h *harness) drain(t *testing.T, session repository.Session) (
	[]entity.DriverEvent, []string, entity.DriverResult,
) {
	t.Helper()

	events := []entity.DriverEvent{}
	logs := []string{}

	waiting, said := session.Events(), session.Logs()

	for waiting != nil || said != nil {
		select {
		case event, open := <-waiting:
			if !open {
				waiting = nil

				continue
			}

			events = append(events, event)
		case line, open := <-said:
			if !open {
				said = nil

				continue
			}

			logs = append(logs, line)
		}
	}

	result, err := session.Wait()
	if err != nil {
		t.Fatalf("wait for the coding agent: %v", err)
	}

	return events, logs, result
}

func (h *harness) replay(t *testing.T, fixture string) (
	[]entity.DriverEvent, []string, entity.DriverResult,
) {
	t.Helper()

	h.replays(t, fixture)

	return h.drain(t, h.start(t, entity.ProfileStandard))
}

func (h *harness) asked(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(h.argv)
	if err != nil {
		t.Fatalf("read what the coding agent was asked: %v", err)
	}

	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}
