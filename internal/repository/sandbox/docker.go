package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/bridge"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
)

const (
	dockerBinary = "docker"

	containerPrefix = "norn-"
	pidDir          = "/run/norn"

	labelRunner = "norn.runner"
	labelRun    = "norn.run"
	labelSpec   = "norn.spec"

	killSettle = time.Second
	killed     = -1
)

type dockerSandbox struct {
	processes repository.Process
	ports     repository.Port
	cfg       config.Docker
	bridged   *bridge.Listener
	runner    string
	user      string
	client    []string
}

func newDocker(
	processes repository.Process,
	ports repository.Port,
	dir *statedir.Dir,
	cfg config.Docker,
	bridged *bridge.Listener,
) *dockerSandbox {
	return &dockerSandbox{
		processes: processes,
		ports:     ports,
		cfg:       cfg,
		bridged:   bridged,
		runner:    fingerprint(dir.Root()),
		user:      strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()),
		client:    clientEnvironment(os.Environ()),
	}
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:6])
}

func clientEnvironment(host []string) []string {
	environment := slices.Clone(host)

	for _, entry := range host {
		if strings.HasPrefix(entry, "DOCKER_CONFIG=") {
			return environment
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		environment = append(environment, "DOCKER_CONFIG="+filepath.Join(home, ".docker"))
	}

	return environment
}

func container(box entity.Sandbox) string {
	return containerPrefix + strings.ToLower(box.Run)
}

func (r *dockerSandbox) docker(
	ctx context.Context,
	timeout time.Duration,
	args ...string,
) (string, error) {
	var out, complaint bytes.Buffer

	code, err := r.processes.Run(ctx, repository.Launch{
		Command:     append([]string{dockerBinary}, args...),
		Environment: r.client,
		Output:      &out,
		Errors:      &complaint,
	}, timeout)
	if err != nil {
		return "", fmt.Errorf("docker %s: %w", args[0], err)
	}

	if code != 0 {
		return "", fmt.Errorf("docker %s: %s", args[0], strings.TrimSpace(complaint.String()))
	}

	return strings.TrimSpace(out.String()), nil
}

func (r *dockerSandbox) Check(ctx context.Context, _ entity.Runtime) error {
	if err := r.bridged.Available(); err != nil {
		return fmt.Errorf("%w: docker cannot be used, because %w", entity.ErrRuntimeUnavailable, err)
	}

	if _, err := r.docker(ctx, r.cfg.Timeout, "version", "--format", "{{.Server.Version}}"); err != nil {
		return fmt.Errorf("%w: docker is not answering (%w)", entity.ErrRuntimeUnavailable, err)
	}

	return nil
}

func (r *dockerSandbox) Has(ctx context.Context, box entity.Sandbox, command string) bool {
	_, err := r.docker(ctx, r.cfg.Timeout, "exec", container(box), "sh", "-c", `command -v "$0"`, command)

	return err == nil
}

func (r *dockerSandbox) Tools(box entity.Sandbox) (string, error) {
	reach, err := r.bridged.Reach()
	if err != nil {
		return "", err
	}

	return reach + entity.RunToolsPath(box.Run), nil
}

func (r *dockerSandbox) Open(ctx context.Context, spec entity.SandboxSpec) error {
	if err := materialise(spec.Protected); err != nil {
		return err
	}

	name := container(spec.Box)
	spec.Protected = present(spec.Protected)

	published, err := r.ports.Block(ctx, spec.Box.Run, r.cfg.Ports)
	if err != nil {
		return err
	}

	spec.Ports = published
	wanted := specFingerprint(r.cfg.Image, spec)

	running, err := r.docker(
		ctx, r.cfg.Timeout, "container", "inspect", "--format",
		"{{.State.Running}} {{index .Config.Labels \""+labelSpec+"\"}}", name,
	)
	if err == nil && running == "true "+wanted {
		return nil
	}

	_, _ = r.docker(ctx, r.cfg.Timeout, "rm", "--force", name)

	if _, err := r.docker(ctx, r.cfg.Timeout, "network", "inspect", name); err != nil {
		if _, err := r.docker(
			ctx, r.cfg.Timeout, "network", "create", "--label", labelRunner+"="+r.runner, name,
		); err != nil {
			return err
		}
	}

	if _, err := r.docker(ctx, r.cfg.Timeout, "image", "inspect", r.cfg.Image); err != nil {
		if _, err := r.docker(ctx, r.cfg.PullTimeout, "pull", "--quiet", r.cfg.Image); err != nil {
			return err
		}
	}

	_, err = r.docker(ctx, r.cfg.Timeout, r.runArgs(name, wanted, spec)...)

	return err
}

func specFingerprint(image string, spec entity.SandboxSpec) string {
	parts := []string{image, spec.Workdir}

	for _, mount := range spec.Mounts {
		parts = append(parts, mount.Path+":"+strconv.FormatBool(mount.ReadOnly))
	}

	for _, port := range spec.Ports {
		parts = append(parts, strconv.Itoa(port))
	}

	parts = append(parts, spec.Protected...)

	return fingerprint(strings.Join(parts, "\n"))
}

func present(paths []string) []string {
	found := make([]string, 0, len(paths))

	for _, path := range paths {
		if exists(path) {
			found = append(found, path)
		}
	}

	return found
}

func (r *dockerSandbox) runArgs(name, wanted string, spec entity.SandboxSpec) []string {
	args := []string{
		"run", "--detach", "--init",
		"--name", name,
		"--hostname", "norn",
		"--network", name,
		"--user", r.user,
		"--security-opt", "no-new-privileges",
		"--tmpfs", pidDir + ":mode=1777",
		"--label", labelRunner + "=" + r.runner,
		"--label", labelRun + "=" + spec.Box.Run,
		"--label", labelSpec + "=" + wanted,
		"--workdir", spec.Workdir,
	}

	if runtime.GOOS == "linux" {
		args = append(args, "--add-host", "host.docker.internal:host-gateway")
	}

	for _, mount := range spec.Mounts {
		volume := mount.Path + ":" + mount.Path
		if mount.ReadOnly {
			volume += ":ro"
		}

		args = append(args, "--volume", volume)
	}

	for _, path := range spec.Protected {
		args = append(args, "--volume", path+":"+path+":ro")
	}

	for _, port := range spec.Ports {
		published := strconv.Itoa(port)
		args = append(args, "--publish", "127.0.0.1:"+published+":"+published)
	}

	return append(args, r.cfg.Image, "sleep", "infinity")
}

func (r *dockerSandbox) Start(
	ctx context.Context,
	box entity.Sandbox,
	launch repository.Launch,
) (repository.Child, error) {
	pidfile := pidDir + "/" + ulid.Make().String() + ".pid"

	child, err := r.processes.Start(ctx, repository.Launch{
		Command:     r.execArgs(box, launch, pidfile),
		Environment: append(slices.Clone(r.client), launch.Environment...),
		Output:      launch.Output,
		Errors:      launch.Errors,
	})
	if err != nil {
		return nil, err
	}

	return &dockerChild{sandbox: r, box: box, pidfile: pidfile, client: child}, nil
}

func (r *dockerSandbox) execArgs(box entity.Sandbox, launch repository.Launch, pidfile string) []string {
	args := []string{dockerBinary, "exec"}

	if launch.Dir != "" {
		args = append(args, "--workdir", launch.Dir)
	}

	for _, entry := range launch.Environment {
		name, _, _ := strings.Cut(entry, "=")
		args = append(args, "--env", name)
	}

	args = append(args, container(box), "setsid", "--fork", "--wait",
		"sh", "-c", `echo $$ > "$0"; exec "$@"`, pidfile)

	return append(args, launch.Command...)
}

func (r *dockerSandbox) Run(
	ctx context.Context,
	box entity.Sandbox,
	launch repository.Launch,
	timeout time.Duration,
) (int, error) {
	if timeout > 0 {
		var stop context.CancelFunc

		ctx, stop = context.WithTimeout(ctx, timeout)
		defer stop()
	}

	child, err := r.Start(ctx, box, launch)
	if err != nil {
		return killed, err
	}

	done := make(chan struct{})

	var (
		code   int
		waited error
	)

	go func() {
		code, waited = child.Wait()

		close(done)
	}()

	select {
	case <-done:
		return code, waited
	case <-ctx.Done():
		_ = child.Stop(context.WithoutCancel(ctx), r.cfg.Timeout)

		return killed, fmt.Errorf("run %s: %w", launch.Command[0], ctx.Err())
	}
}

func (r *dockerSandbox) signal(ctx context.Context, box entity.Sandbox, pidfile, signal string) error {
	_, err := r.docker(
		ctx, r.cfg.Timeout, "exec", container(box), "sh", "-c",
		`[ -f "$0" ] && kill -`+signal+` "-$(cat "$0")"`, pidfile,
	)

	return err
}

func (r *dockerSandbox) Close(ctx context.Context, box entity.Sandbox) error {
	name := container(box)

	_, removed := r.docker(ctx, r.cfg.Timeout, "rm", "--force", name)
	_, unnetworked := r.docker(ctx, r.cfg.Timeout, "network", "rm", name)

	r.ports.Release(ctx, box.Run)

	if removed != nil && !strings.Contains(removed.Error(), "No such container") {
		return removed
	}

	if unnetworked != nil && !strings.Contains(unnetworked.Error(), "not found") {
		return unnetworked
	}

	return nil
}

func (r *dockerSandbox) Sweep(ctx context.Context) error {
	filter := "label=" + labelRunner + "=" + r.runner

	containers, err := r.docker(ctx, r.cfg.Timeout, "ps", "--all", "--quiet", "--filter", filter)
	if err != nil {
		return err
	}

	failures := []error{}

	for _, id := range strings.Fields(containers) {
		if _, err := r.docker(ctx, r.cfg.Timeout, "rm", "--force", id); err != nil {
			failures = append(failures, err)
		}
	}

	networks, err := r.docker(ctx, r.cfg.Timeout, "network", "ls", "--quiet", "--filter", filter)
	if err != nil {
		return errors.Join(append(failures, err)...)
	}

	for _, id := range strings.Fields(networks) {
		if _, err := r.docker(ctx, r.cfg.Timeout, "network", "rm", id); err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

type dockerChild struct {
	sandbox *dockerSandbox
	box     entity.Sandbox
	pidfile string
	client  repository.Child
}

func (c *dockerChild) PID() int {
	return c.client.PID()
}

func (c *dockerChild) Wait() (int, error) {
	return c.client.Wait()
}

func (c *dockerChild) Stop(ctx context.Context, grace time.Duration) error {
	exited := make(chan struct{})

	go func() {
		_, _ = c.client.Wait()

		close(exited)
	}()

	if err := c.sandbox.signal(ctx, c.box, c.pidfile, "TERM"); err != nil {
		return c.client.Stop(ctx, grace)
	}

	deadline := time.NewTimer(grace)
	defer deadline.Stop()

	select {
	case <-exited:
		return nil
	case <-deadline.C:
	}

	_ = c.sandbox.signal(ctx, c.box, c.pidfile, "KILL")

	select {
	case <-exited:
		return nil
	case <-time.After(killSettle):
		return c.client.Stop(ctx, grace)
	}
}
