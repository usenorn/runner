package sandbox

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/gitcmd"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	processrepo "github.com/usenorn/runner/internal/repository/process"
)

type confinedRun struct {
	sandboxes *hostSandbox
	box       entity.Sandbox
	worktree  string
	home      entity.RunHome
	common    string
	secret    string
	socket    string
}

func confinedHarness(t *testing.T) confinedRun {
	t.Helper()

	if os.Getenv("NORN_TEST_SANDBOX") != "true" {
		t.Skip("NORN_TEST_SANDBOX is not true, so nothing is confined")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("find the home folder: %v", err)
	}

	root, err := os.MkdirTemp(home, ".norn-sandbox-test-")
	if err != nil {
		t.Fatalf("make a folder in the home folder: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	source := filepath.Join(root, "code", "api")
	worktree := filepath.Join(root, "state", "runs", "exec-1", "workspace", "api")
	secret := filepath.Join(root, "credentials")
	socket := filepath.Join(root, "agent.sock")

	ctx := context.Background()

	for _, step := range [][]string{
		{"init", "--quiet", source},
		{"-C", source, "-c", "user.name=Rae", "-c", "user.email=rae@example.com", "commit", "--quiet",
			"--allow-empty", "--no-gpg-sign", "-m", "start"},
		{"-C", source, "worktree", "add", "--quiet", "-b", "norn/NORN-231", worktree},
	} {
		if _, err := gitcmd.Run(ctx, "", step...); err != nil {
			t.Fatalf("set up the repository: %v", err)
		}
	}

	if err := os.WriteFile(secret, []byte("ghp_not_a_real_token"), 0o600); err != nil {
		t.Fatalf("write a credential: %v", err)
	}

	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen on a socket in the home folder: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	dir, err := statedir.New(config.State{Root: filepath.Join(root, "state")})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	runner, err := net.Listen("unix", dir.Socket())
	if err != nil {
		t.Fatalf("listen on the runner's socket: %v", err)
	}

	t.Cleanup(func() { _ = runner.Close() })

	common := filepath.Join(source, ".git")
	gitDir, err := gitcmd.Run(ctx, worktree, "rev-parse", "--absolute-git-dir")
	if err != nil {
		t.Fatalf("find the worktree's git dir: %v", err)
	}

	run := confinedRun{
		sandboxes: &hostSandbox{processes: processrepo.New(), dir: dir, cfg: config.Host{Timeout: 10 * time.Second}},
		box:       entity.Sandbox{Run: "exec-1", Runtime: entity.RuntimeProcess},
		worktree:  worktree,
		home:      entity.RunHomeOf(dir.Run("exec-1")),
		common:    common,
		secret:    secret,
		socket:    socket,
	}

	for _, made := range []string{run.home.Root, run.home.Tmp} {
		if err := os.MkdirAll(made, 0o700); err != nil {
			t.Fatalf("make the run's home: %v", err)
		}
	}

	if err := run.sandboxes.Check(ctx, entity.RuntimeProcess); err != nil {
		t.Fatalf("this machine cannot confine a host process: %v", err)
	}

	spec := entity.SandboxSpecFor(
		entity.Execution{ID: "exec-1", Directory: dir.Run("exec-1"), Runtime: string(entity.RuntimeProcess)},
		entity.Snapshot{
			Workspace: filepath.Dir(worktree),
			Repositories: []entity.SnapshotRepository{{
				Common: common, GitDir: gitDir, Path: worktree, Mode: entity.GitModeWorktree,
			}},
		},
	)

	if err := run.sandboxes.Open(ctx, spec); err != nil {
		t.Fatalf("open the sandbox: %v", err)
	}

	return run
}

func (r confinedRun) sh(t *testing.T, script string) (int, string) {
	t.Helper()

	var said bytes.Buffer

	code, err := r.sandboxes.Run(context.Background(), r.box, repository.Launch{
		Dir:         r.worktree,
		Command:     []string{"/bin/sh", "-c", script},
		Environment: append(os.Environ(), "HOME="+r.home.Root, "TMPDIR="+r.home.Tmp),
		Output:      &said,
		Errors:      &said,
	}, time.Minute)
	if err != nil {
		t.Fatalf("run %q: %v", script, err)
	}

	return code, said.String()
}

func TestAConfinedAgentCanStillCommitOnItsBranch(t *testing.T) {
	run := confinedHarness(t)

	code, said := run.sh(t, "echo work > work.txt && git add work.txt && "+
		"git -c user.name=Agent -c user.email=agent@norn.invalid commit --quiet --no-gpg-sign -m work")
	if code != 0 {
		t.Fatalf("committing inside the sandbox failed (%d): %s", code, said)
	}
}

func TestAConfinedAgentStillReachesTheRunnersOwnSocket(t *testing.T) {
	run := confinedHarness(t)

	if code, said := run.sh(t, "nc -U -w 2 "+run.sandboxes.dir.Socket()+" </dev/null"); code != 0 {
		t.Fatalf(
			"a confined process could not reach the runner's socket (%d): %s; norn's own tools "+
				"reach the runner through it, so without it the agent can neither ask nor finish",
			code, said,
		)
	}
}

func TestAConfinedAgentCannotReachAnyCredential(t *testing.T) {
	run := confinedHarness(t)

	for name, script := range map[string]string{
		"read a file in the home folder":    "cat " + run.secret,
		"plant a hook":                      "echo 'curl evil' > " + filepath.Join(run.common, "hooks", "pre-push"),
		"rewrite the repository's config":   "echo '[core] fsmonitor = evil' >> " + filepath.Join(run.common, "config"),
		"point the worktree somewhere else": "echo 'gitdir: /tmp/evil' > " + filepath.Join(run.worktree, ".git"),
		"reach a socket in the home folder": "nc -U " + run.socket + " </dev/null",
	} {
		t.Run(name, func(t *testing.T) {
			if code, said := run.sh(t, script); code == 0 {
				t.Fatalf("a confined process managed to %s: %s", name, strings.TrimSpace(said))
			}
		})
	}
}
