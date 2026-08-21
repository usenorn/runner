package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
)

func write(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, "runner.yaml")

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func TestDefaultsProduceAUsableConfigWithoutAnyFile(t *testing.T) {
	t.Setenv("NORN_STATE_ROOT", t.TempDir())

	cfg, err := config.New("", config.Overrides{})
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}

	if cfg.Runner.Capacity != 2 {
		t.Fatalf("capacity defaulted to %d, want 2", cfg.Runner.Capacity)
	}

	if cfg.Runner.Runtime != config.RuntimeAuto {
		t.Fatalf("runtime defaulted to %q, want auto", cfg.Runner.Runtime)
	}

	if cfg.Runner.PortRange != [2]int{43000, 44999} {
		t.Fatalf("port range defaulted to %v, want [43000 44999]", cfg.Runner.PortRange)
	}

	if cfg.Runner.Retention.RunsMaxAge != 14*24*time.Hour {
		t.Fatalf("runs_max_age defaulted to %s, want 336h", cfg.Runner.Retention.RunsMaxAge)
	}
}

func TestTheFlatTopLevelKeysOfTheSpecReachTheRunnerSection(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NORN_STATE_ROOT", root)

	write(t, root, `
version: 1
server: https://norn.example
capacity: 5
runtime: docker
port_range: [50000, 50100]
retention:
  workspace_after_done: 45m
  runs_max_age: 14d
  runs_max_disk: 20GB
telemetry: minimal
`)

	cfg, err := config.New("", config.Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Runner.Server != "https://norn.example" || cfg.Runner.Capacity != 5 {
		t.Fatalf("squashed runner section did not load: %+v", cfg.Runner)
	}

	if cfg.Runner.Runtime != config.RuntimeDocker || cfg.Runner.Telemetry != config.TelemetryMinimal {
		t.Fatalf("runtime/telemetry did not load: %+v", cfg.Runner)
	}

	if cfg.Runner.PortRange != [2]int{50000, 50100} {
		t.Fatalf("port range loaded as %v", cfg.Runner.PortRange)
	}

	if cfg.Runner.Retention.RunsMaxAge != 14*24*time.Hour {
		t.Fatalf("14d loaded as %s, want 336h", cfg.Runner.Retention.RunsMaxAge)
	}

	if cfg.Runner.Retention.RunsMaxDisk != 20<<30 {
		t.Fatalf("20GB loaded as %d, want %d", cfg.Runner.Retention.RunsMaxDisk, 20<<30)
	}

	if cfg.Runner.Retention.WorkspaceAfterDone != 45*time.Minute {
		t.Fatalf("workspace_after_done loaded as %s", cfg.Runner.Retention.WorkspaceAfterDone)
	}
}

func TestAConfigFileNamedByTheFlagIsReadAndAMissingOneIsAnError(t *testing.T) {
	t.Setenv("NORN_STATE_ROOT", t.TempDir())

	named := filepath.Join(t.TempDir(), "elsewhere.yaml")
	if err := os.WriteFile(named, []byte("capacity: 9\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.New(named, config.Overrides{})
	if err != nil {
		t.Fatalf("load named config: %v", err)
	}

	if cfg.Runner.Capacity != 9 {
		t.Fatalf("capacity is %d, want the named file's 9", cfg.Runner.Capacity)
	}

	if _, err := config.New(filepath.Join(t.TempDir(), "absent.yaml"), config.Overrides{}); err == nil {
		t.Fatalf("a config file the operator named but which does not exist was ignored")
	}
}

func TestAMissingRunnerYamlInTheStateDirectoryIsNotAnError(t *testing.T) {
	t.Setenv("NORN_STATE_ROOT", t.TempDir())

	if _, err := config.New("", config.Overrides{}); err != nil {
		t.Fatalf("a first run with no config file failed: %v", err)
	}
}

func TestTheEnvironmentBeatsTheFileAndAFlagBeatsTheEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NORN_STATE_ROOT", root)

	write(t, root, "capacity: 3\nruntime: process\n")

	cfg, err := config.New("", config.Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Runner.Capacity != 3 {
		t.Fatalf("file capacity is %d, want 3", cfg.Runner.Capacity)
	}

	t.Setenv("NORN_CAPACITY", "7")

	cfg, err = config.New("", config.Overrides{})
	if err != nil {
		t.Fatalf("load with env: %v", err)
	}

	if cfg.Runner.Capacity != 7 {
		t.Fatalf("environment capacity is %d, want 7", cfg.Runner.Capacity)
	}

	overridden := 11
	docker := config.RuntimeDocker

	cfg, err = config.New("", config.Overrides{Capacity: &overridden, Runtime: &docker})
	if err != nil {
		t.Fatalf("load with overrides: %v", err)
	}

	if cfg.Runner.Capacity != 11 || cfg.Runner.Runtime != config.RuntimeDocker {
		t.Fatalf("flags did not win: capacity %d runtime %q", cfg.Runner.Capacity, cfg.Runner.Runtime)
	}
}

func TestAPortRangeWrittenAsAStringInTheEnvironmentIsUnderstood(t *testing.T) {
	t.Setenv("NORN_STATE_ROOT", t.TempDir())
	t.Setenv("NORN_PORT_RANGE", "50000-50500")

	cfg, err := config.New("", config.Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Runner.PortRange != [2]int{50000, 50500} {
		t.Fatalf("port range is %v, want [50000 50500]", cfg.Runner.PortRange)
	}
}

func TestTheConfigFileThatWasReadIsReported(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NORN_STATE_ROOT", root)

	path := write(t, root, "capacity: 2\n")

	cfg, err := config.New("", config.Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.State.ConfigFile != path {
		t.Fatalf("reported config file %q, want %q", cfg.State.ConfigFile, path)
	}
}

func TestAConfigurationThatCannotWorkIsRefused(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "a version this binary does not know", body: "version: 2\n"},
		{name: "a capacity below one", body: "capacity: 0\n"},
		{name: "an unknown runtime", body: "runtime: kubernetes\n"},
		{name: "an unknown telemetry level", body: "telemetry: chatty\n"},
		{name: "a server that is not a url", body: "server: not a url\n"},
		{name: "a server that is not http", body: "server: ftp://norn.example\n"},
		{name: "a port range that reserves nothing", body: "port_range: [44999, 43000]\n"},
		{name: "a port range needing privileges", body: "port_range: [80, 44999]\n"},
		{name: "a retention age of zero", body: "retention:\n  runs_max_age: 0s\n"},
		{name: "an unknown console setting", body: "log:\n  console: sometimes\n"},
		{
			name: "a drain shorter than a request",
			body: "control:\n  request_timeout: 30s\n  shutdown_timeout: 5s\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("NORN_STATE_ROOT", root)

			write(t, root, testCase.body)

			if _, err := config.New("", config.Overrides{}); err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
		})
	}
}

func TestASizeWrittenWithAUnitTheRunnerDoesNotKnowIsRefused(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NORN_STATE_ROOT", root)

	write(t, root, "retention:\n  runs_max_disk: 20 parsecs\n")

	if _, err := config.New("", config.Overrides{}); err == nil {
		t.Fatalf("a size written in parsecs was accepted")
	}
}

func TestNoConfigFileIsReportedAsNoneRatherThanAPathThatDoesNotExist(t *testing.T) {
	t.Setenv("NORN_STATE_ROOT", t.TempDir())

	cfg, err := config.New("", config.Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.State.ConfigFile != "" {
		t.Fatalf(
			"reported %q as the config file when none was read; an operator would go looking "+
				"for a file that is not there",
			cfg.State.ConfigFile,
		)
	}
}
