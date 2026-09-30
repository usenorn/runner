package sandbox

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/bridge"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	portrepo "github.com/usenorn/runner/internal/repository/port"
	processrepo "github.com/usenorn/runner/internal/repository/process"
)

type dockerHarness struct {
	sandbox  *dockerSandbox
	mu       sync.Mutex
	ran      [][]string
	started  []repository.Launch
	answers  map[string]string
	failures map[string]bool
}

func newDockerHarness(t *testing.T) *dockerHarness {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	ctrl := gomock.NewController(t)
	processes := processrepo.NewMockProcess(ctrl)
	h := &dockerHarness{answers: map[string]string{}, failures: map[string]bool{}}

	processes.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, launch repository.Launch, _ time.Duration) (int, error) {
			h.mu.Lock()
			defer h.mu.Unlock()

			h.ran = append(h.ran, launch.Command)
			asked := strings.Join(launch.Command[1:3], " ")

			if h.failures[asked] {
				_, _ = io.WriteString(launch.Errors, "Error: No such object")

				return 1, nil
			}

			_, _ = io.WriteString(launch.Output, h.answers[asked])

			return 0, nil
		}).
		AnyTimes()

	processes.EXPECT().
		Start(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, launch repository.Launch) (repository.Child, error) {
			h.mu.Lock()
			defer h.mu.Unlock()

			h.started = append(h.started, launch)

			return processrepo.NewMockChild(ctrl), nil
		}).
		AnyTimes()

	h.sandbox = newDocker(
		processes,
		portrepo.New(config.Runner{PortRange: [2]int{45800, 45899}}),
		dir,
		config.Docker{
			Image: "ghcr.io/usenorn/runner-sandbox:test", Ports: 3,
			Timeout: time.Second, PullTimeout: time.Second,
		},
		listening(t),
	)

	return h
}

func (h *dockerHarness) asked(verb string) [][]string {
	h.mu.Lock()
	defer h.mu.Unlock()

	found := [][]string{}

	for _, command := range h.ran {
		if strings.HasPrefix(strings.Join(command[1:], " "), verb) {
			found = append(found, command)
		}
	}

	return found
}

func spec() entity.SandboxSpec {
	return entity.SandboxSpec{
		Box:     entity.Sandbox{Run: "exec-01ABC", Runtime: entity.RuntimeDocker},
		Workdir: "/state/runs/exec-01ABC/workspace",
		Mounts: []entity.Mount{
			{Path: "/state/runs/exec-01ABC/workspace"},
			{Path: "/state/runs/exec-01ABC/metadata", ReadOnly: true},
		},
	}
}

func TestARunsContainerIsItsOwnWithTheRunsFoldersAndPortsAndNothingMore(t *testing.T) {
	h := newDockerHarness(t)
	h.failures["container inspect"] = true
	h.failures["network inspect"] = true

	if err := h.sandbox.Open(context.Background(), spec()); err != nil {
		t.Fatalf("open the run's container: %v", err)
	}

	created := h.asked("run ")
	if len(created) != 1 {
		t.Fatalf("the container was created %d times", len(created))
	}

	args := strings.Join(created[0], " ")

	for _, want := range []string{
		"--name norn-exec-01abc",
		"--network norn-exec-01abc",
		"--security-opt no-new-privileges",
		"--volume /state/runs/exec-01ABC/workspace:/state/runs/exec-01ABC/workspace",
		"--volume /state/runs/exec-01ABC/metadata:/state/runs/exec-01ABC/metadata:ro",
		"--publish 127.0.0.1:",
		"--label norn.run=exec-01ABC",
		"ghcr.io/usenorn/runner-sandbox:test sleep infinity",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("the container was started without %q:\n%s", want, args)
		}
	}

	if published := strings.Count(args, "--publish"); published != 3 {
		t.Fatalf("the container publishes %d ports, want the 3 docker.ports asks for", published)
	}

	if len(h.asked("network create")) != 1 {
		t.Fatal("the run's container was not given a network of its own, so every run could reach every other")
	}
}

func TestTheAgentsContainerCannotRewriteTheRepositorysConfigOrHooks(t *testing.T) {
	h := newDockerHarness(t)
	h.failures["container inspect"] = true
	h.failures["network inspect"] = true

	common := t.TempDir()
	config := filepath.Join(common, "config")
	hooks := filepath.Join(common, "hooks")

	if err := os.WriteFile(config, []byte("[core]\n"), 0o600); err != nil {
		t.Fatalf("write a config: %v", err)
	}

	if err := os.Mkdir(hooks, 0o755); err != nil {
		t.Fatalf("make the hooks: %v", err)
	}

	protected := spec()
	protected.Mounts = append(protected.Mounts, entity.Mount{Path: common})
	protected.Protected = []string{config, hooks, filepath.Join(common, "config.worktree")}

	if err := h.sandbox.Open(context.Background(), protected); err != nil {
		t.Fatalf("open the run's container: %v", err)
	}

	args := strings.Join(h.asked("run ")[0], " ")

	for _, want := range []string{
		"--volume " + common + ":" + common + " ",
		"--volume " + config + ":" + config + ":ro",
		"--volume " + hooks + ":" + hooks + ":ro",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("the container was started without %q:\n%s", want, args)
		}
	}

	if strings.Contains(args, "config.worktree") {
		t.Fatalf("a path that does not exist was handed to docker, which would refuse to start:\n%s", args)
	}
}

func TestAContainerThatIsAlreadyRunningAsWantedIsKept(t *testing.T) {
	h := newDockerHarness(t)
	h.failures["network inspect"] = true
	ctx := context.Background()

	if err := h.sandbox.Open(ctx, spec()); err != nil {
		t.Fatalf("open: %v", err)
	}

	created := strings.Join(h.asked("run ")[0], " ")
	wanted := created[strings.Index(created, "norn.spec=")+len("norn.spec="):]
	wanted, _, _ = strings.Cut(wanted, " ")

	h.answers["container inspect"] = "true " + wanted

	if err := h.sandbox.Open(ctx, spec()); err != nil {
		t.Fatalf("open again: %v", err)
	}

	if created := h.asked("run "); len(created) != 1 {
		t.Fatalf(
			"a container already running as wanted was replaced; everything the run had " +
				"started inside it would be killed on every resume",
		)
	}
}

func TestWorkInAContainerIsHandedItsEnvironmentByNameNeverOnTheCommandLine(t *testing.T) {
	h := newDockerHarness(t)

	_, err := h.sandbox.Start(context.Background(), spec().Box, repository.Launch{
		Dir:         "/state/runs/exec-01ABC/workspace",
		Command:     []string{"claude", "--print", "do the work"},
		Environment: []string{"HOME=/state/runs/exec-01ABC/home", "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-secret"},
	})
	if err != nil {
		t.Fatalf("start work in the container: %v", err)
	}

	started := h.started[0]
	args := strings.Join(started.Command, " ")

	if strings.Contains(args, "sk-ant-oat01-secret") {
		t.Fatalf("a secret went on the command line, where anybody listing processes reads it: %s", args)
	}

	for _, want := range []string{
		"docker exec --workdir /state/runs/exec-01ABC/workspace",
		"--env HOME --env CLAUDE_CODE_OAUTH_TOKEN",
		"norn-exec-01abc setsid --fork --wait",
		"claude --print do the work",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("the work was started without %q:\n%s", want, args)
		}
	}

	if !slices.Contains(started.Environment, "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-secret") {
		t.Fatal("the value the container reads by name was never given to the docker client")
	}

	if !slices.ContainsFunc(started.Environment, func(entry string) bool {
		return strings.HasPrefix(entry, "DOCKER_CONFIG=")
	}) {
		t.Fatal(
			"the docker client was started without its own config, so a run's home would hide " +
				"which docker it is meant to talk to",
		)
	}
}

func TestSweepingClearsEveryContainerAndNetworkThisRunnerLeftBehind(t *testing.T) {
	h := newDockerHarness(t)
	h.answers["ps --all"] = "c1\nc2"
	h.answers["network ls"] = "n1"

	if err := h.sandbox.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if removed := len(h.asked("rm --force")); removed != 2 {
		t.Fatalf("%d containers were removed, want both this runner left behind", removed)
	}

	if removed := len(h.asked("network rm")); removed != 1 {
		t.Fatalf("%d networks were removed, want the one this runner left behind", removed)
	}

	for _, listed := range h.asked("ps --all") {
		if !strings.Contains(strings.Join(listed, " "), "label=norn.runner="+h.sandbox.runner) {
			t.Fatalf("the sweep listed containers that are not this runner's: %v", listed)
		}
	}
}

func listening(t *testing.T) *bridge.Listener {
	t.Helper()

	bridged, closeBridge, err := bridge.New(config.Docker{Bridge: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("listen for containers: %v", err)
	}

	t.Cleanup(closeBridge)

	return bridged
}
