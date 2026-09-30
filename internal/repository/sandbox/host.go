package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
)

var errSandboxNotOpen = errors.New("this run's sandbox was never opened, so nothing may run in it")

type hostSandbox struct {
	processes repository.Process
	dir       *statedir.Dir
	cfg       config.Host
	policies  sync.Map
}

func (r *hostSandbox) Check(ctx context.Context, _ entity.Runtime) error {
	policy, err := r.policyFor(entity.SandboxSpec{})
	if err != nil {
		return fmt.Errorf("%w: %w", entity.ErrRuntimeUnavailable, err)
	}

	probe, err := r.confine(policy, []string{"/usr/bin/true"})
	if err != nil {
		return fmt.Errorf("%w: %w", entity.ErrRuntimeUnavailable, err)
	}

	code, err := r.processes.Run(ctx, repository.Launch{Command: probe}, r.cfg.Timeout)
	if err != nil || code != 0 {
		return fmt.Errorf(
			"%w: host processes cannot be confined on this machine, so none is started (%s exited %d: %v)",
			entity.ErrRuntimeUnavailable, probe[0], code, err,
		)
	}

	return nil
}

func (r *hostSandbox) Open(_ context.Context, spec entity.SandboxSpec) error {
	policy, err := r.policyFor(spec)
	if err != nil {
		return err
	}

	r.policies.Store(spec.Box.Run, policy)

	return nil
}

func (r *hostSandbox) Start(
	ctx context.Context,
	box entity.Sandbox,
	launch repository.Launch,
) (repository.Child, error) {
	confined, err := r.confined(box, launch)
	if err != nil {
		return nil, err
	}

	return r.processes.Start(ctx, confined)
}

func (r *hostSandbox) Run(
	ctx context.Context,
	box entity.Sandbox,
	launch repository.Launch,
	timeout time.Duration,
) (int, error) {
	confined, err := r.confined(box, launch)
	if err != nil {
		return 0, err
	}

	return r.processes.Run(ctx, confined, timeout)
}

func (r *hostSandbox) Has(_ context.Context, _ entity.Sandbox, command string) bool {
	_, err := exec.LookPath(command)

	return err == nil
}

func (r *hostSandbox) Tools(entity.Sandbox) (string, error) {
	return "", nil
}

func (r *hostSandbox) Close(_ context.Context, box entity.Sandbox) error {
	r.policies.Delete(box.Run)

	return nil
}

func (r *hostSandbox) Sweep(context.Context) error {
	return nil
}

func (r *hostSandbox) confined(box entity.Sandbox, launch repository.Launch) (repository.Launch, error) {
	held, found := r.policies.Load(box.Run)
	if !found {
		return repository.Launch{}, fmt.Errorf("%w: %s", errSandboxNotOpen, box.Run)
	}

	policy, _ := held.(hostPolicy)

	command, err := r.confine(policy, launch.Command)
	if err != nil {
		return repository.Launch{}, err
	}

	launch.Command = command

	return launch, nil
}

func (r *hostSandbox) confine(policy hostPolicy, command []string) ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		return append([]string{seatbeltBinary, "-p", seatbelt(policy)}, command...), nil
	case "linux":
		binary, err := exec.LookPath(bwrapBinary)
		if err != nil {
			return nil, fmt.Errorf("bubblewrap is not installed: %w", err)
		}

		return bubblewrap(policy, binary, command), nil
	default:
		return nil, fmt.Errorf("host processes cannot be confined on %s", runtime.GOOS)
	}
}

func (r *hostSandbox) policyFor(spec entity.SandboxSpec) (hostPolicy, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return hostPolicy{}, fmt.Errorf("find the home folder to keep out of the sandbox: %w", err)
	}

	policy := hostPolicy{
		home:      canonical(home),
		state:     canonical(r.dir.Root()),
		socket:    canonical(r.dir.Socket()),
		protected: canonicals(spec.Protected),
	}

	for _, mount := range spec.Mounts {
		if mount.ReadOnly {
			policy.readable = append(policy.readable, canonical(mount.Path))
			policy.protected = append(policy.protected, canonical(mount.Path))

			continue
		}

		policy.writable = append(policy.writable, canonical(mount.Path))
	}

	for _, toolchain := range entity.Toolchains() {
		root := filepath.Join(home, toolchain.Dir)
		if !exists(root) {
			continue
		}

		policy.readable = append(policy.readable, canonical(root))
	}

	policy.readable = append(policy.readable, installed(home)...)
	policy.readable = append(policy.readable, canonicals(expanded(home, r.cfg.Readable))...)

	slices.Sort(policy.readable)
	policy.readable = slices.Compact(policy.readable)

	return policy, nil
}

func installed(home string) []string {
	found := make([]string, 0, 8)

	if executable, err := os.Executable(); err == nil {
		found = append(found, filepath.Dir(canonical(executable)))
	}

	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if !beneath(home, entry) || !exists(entry) {
			continue
		}

		found = append(found, canonical(entry))
		found = append(found, linkedFrom(home, entry)...)
	}

	return found
}

func linkedFrom(home, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	found := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink == 0 {
			continue
		}

		target, err := filepath.EvalSymlinks(filepath.Join(dir, entry.Name()))
		if err != nil || !beneath(home, filepath.Dir(target)) {
			continue
		}

		found = append(found, filepath.Dir(canonical(target)))
	}

	return found
}

func expanded(home string, paths []string) []string {
	resolved := make([]string, 0, len(paths))

	for _, path := range paths {
		trimmed := strings.TrimSpace(path)

		switch {
		case trimmed == "":
			continue
		case trimmed == "~":
			trimmed = home
		case strings.HasPrefix(trimmed, "~/"):
			trimmed = filepath.Join(home, trimmed[2:])
		}

		resolved = append(resolved, trimmed)
	}

	return resolved
}

func within(root, path string) bool {
	relative, err := filepath.Rel(canonical(root), canonical(path))

	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func beneath(root, path string) bool {
	return within(root, path) && canonical(root) != canonical(path)
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func canonicals(paths []string) []string {
	resolved := make([]string, 0, len(paths))

	for _, path := range paths {
		resolved = append(resolved, canonical(path))
	}

	return resolved
}

func canonical(path string) string {
	if path == "" {
		return ""
	}

	cleaned := filepath.Clean(path)

	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved
	}

	parent, name := filepath.Split(cleaned)
	if parent == cleaned || parent == "" {
		return cleaned
	}

	return filepath.Join(canonical(strings.TrimSuffix(parent, string(filepath.Separator))), name)
}
