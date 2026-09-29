package sandbox

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	portrepo "github.com/usenorn/runner/internal/repository/port"
	processrepo "github.com/usenorn/runner/internal/repository/process"
)

const liveImage = "debian:bookworm-slim"

func liveDocker(t *testing.T) (*dockerSandbox, entity.SandboxSpec) {
	t.Helper()

	if os.Getenv("NORN_TEST_DOCKER") != "true" {
		t.Skip("NORN_TEST_DOCKER is not true, so no container is started")
	}

	root, err := os.MkdirTemp(os.Getenv("HOME"), ".norn-docker-test-")
	if err != nil {
		t.Fatalf("make a folder docker can share: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	sandbox := newDocker(
		processrepo.New(),
		portrepo.New(config.Runner{PortRange: [2]int{45900, 45999}}),
		dir,
		config.Docker{Image: liveImage, Ports: 2, Timeout: time.Minute, PullTimeout: 10 * time.Minute},
	)

	workspace := dir.Run("exec-01live") + "/workspace"
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("make a workspace: %v", err)
	}

	box := entity.Sandbox{Run: "exec-01live", Runtime: entity.RuntimeDocker}

	t.Cleanup(func() { _ = sandbox.Close(context.WithoutCancel(t.Context()), box) })

	return sandbox, entity.SandboxSpec{Box: box, Workdir: workspace, Mounts: []entity.Mount{{Path: workspace}}}
}

func TestWorkRunsInsideTheRunsContainerAndWritesIntoItsWorkspace(t *testing.T) {
	sandbox, spec := liveDocker(t)
	ctx := t.Context()

	if err := sandbox.Open(ctx, spec); err != nil {
		t.Fatalf("open: %v", err)
	}

	var said bytes.Buffer

	code, err := sandbox.Run(ctx, spec.Box, repository.Launch{
		Dir:         spec.Workdir,
		Command:     []string{"sh", "-c", `cat /etc/debian_version >/dev/null && echo "$GREETING" > made.txt && echo inside`},
		Environment: []string{"GREETING=from the task"},
		Output:      &said,
	}, time.Minute)
	if err != nil || code != 0 {
		t.Fatalf("run inside the container came back %d, %v: %s", code, err, said.String())
	}

	made, err := os.ReadFile(spec.Workdir + "/made.txt")
	if err != nil || strings.TrimSpace(string(made)) != "from the task" {
		t.Fatalf("the workspace holds %q (%v), want what the work wrote from inside", made, err)
	}
}

func TestStoppingWorkInAContainerStopsEverythingItStarted(t *testing.T) {
	sandbox, spec := liveDocker(t)
	ctx := t.Context()

	if err := sandbox.Open(ctx, spec); err != nil {
		t.Fatalf("open: %v", err)
	}

	child, err := sandbox.Start(ctx, spec.Box, repository.Launch{
		Command: []string{"sh", "-c", "sleep 300 & sleep 300 & wait"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	time.Sleep(time.Second)

	if err := child.Stop(ctx, 5*time.Second); err != nil {
		t.Fatalf("stop: %v", err)
	}

	out, err := exec.CommandContext(ctx, "docker", "exec", container(spec.Box), "sh", "-c", "cat /proc/[0-9]*/comm 2>/dev/null | grep -c sleep || true").Output()
	if err != nil {
		t.Fatalf("list what is left: %v", err)
	}

	if left := strings.TrimSpace(string(out)); left != "1" {
		t.Fatalf(
			"%s sleeps are still running in the container besides its own; stopping a service "+
				"has to stop everything it started, or the next start collides with it",
			left,
		)
	}
}

func TestAServiceListeningInsideTheContainerIsReachedOnTheHostAtTheSamePort(t *testing.T) {
	sandbox, spec := liveDocker(t)
	ctx := t.Context()

	if err := sandbox.Open(ctx, spec); err != nil {
		t.Fatalf("open: %v", err)
	}

	port, err := sandbox.ports.Reserve(ctx, spec.Box.Run, "web")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}

	listen := `use IO::Socket::INET; my $s = IO::Socket::INET->new(LocalAddr => "0.0.0.0", ` +
		`LocalPort => $ARGV[0], Listen => 1, ReuseAddr => 1) or die; ` +
		`while (my $c = $s->accept) { print $c "served from inside\n"; close $c }`

	child, err := sandbox.Start(ctx, spec.Box, repository.Launch{
		Command: []string{"perl", "-e", listen, strconv.Itoa(port)},
	})
	if err != nil {
		t.Fatalf("start the service: %v", err)
	}

	t.Cleanup(func() { _ = child.Stop(context.WithoutCancel(ctx), time.Second) })

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	var said string

	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		connection, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			continue
		}

		reply, _ := io.ReadAll(connection)
		_ = connection.Close()

		if said = string(reply); said != "" {
			break
		}
	}

	if !strings.Contains(said, "served from inside") {
		t.Fatalf(
			"the host read %q at %s. A preview and a health check both reach a service at the "+
				"port it was given, so a container has to publish it at that same port",
			said, address,
		)
	}
}
