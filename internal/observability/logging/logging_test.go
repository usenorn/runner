package logging_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

func newLogger(t *testing.T, level string) (*statedir.Dir, func()) {
	t.Helper()

	dir, err := statedir.New(config.State{Root: filepath.Join(t.TempDir(), "norn")})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	logger, cleanup, err := logging.New(
		config.App{Version: "test", LogLevel: level},
		config.Log{Console: config.ConsoleNever, MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1},
		dir,
	)
	if err != nil {
		t.Fatalf("build logger: %v", err)
	}

	logger.InfoContext(context.Background(), "the runner said something", "token", "nrn_secret")

	return dir, cleanup
}

func TestALogLineReachesTheRotatingFileWithTheRunnerStamped(t *testing.T) {
	dir, cleanup := newLogger(t, "info")
	cleanup()

	raw, err := os.ReadFile(dir.LogFile())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	var line map[string]any

	if err := json.Unmarshal([]byte(strings.SplitN(string(raw), "\n", 2)[0]), &line); err != nil {
		t.Fatalf("the log is not json: %v", err)
	}

	if line["service"] != "norn-runner" || line["version"] != "test" {
		t.Fatalf("the line is not stamped with the runner: %v", line)
	}

	if _, ok := line["pid"]; !ok {
		t.Fatalf("the line does not carry the pid, so two daemons could not be told apart")
	}
}

func TestASecretPassedToTheLoggerNeverReachesTheFile(t *testing.T) {
	dir, cleanup := newLogger(t, "info")
	cleanup()

	raw, err := os.ReadFile(dir.LogFile())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if strings.Contains(string(raw), "nrn_secret") {
		t.Fatalf("a token reached the log file:\n%s", raw)
	}

	if !strings.Contains(string(raw), logging.Redacted) {
		t.Fatalf("the token was neither redacted nor written; the field vanished silently")
	}
}

func TestALogLevelTheRunnerDoesNotKnowIsRefused(t *testing.T) {
	dir, err := statedir.New(config.State{Root: filepath.Join(t.TempDir(), "norn")})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	_, _, err = logging.New(
		config.App{Version: "test", LogLevel: "chatty"},
		config.Log{Console: config.ConsoleNever, MaxSizeMB: 1},
		dir,
	)
	if err == nil {
		t.Fatalf("an unknown log level was accepted")
	}
}
