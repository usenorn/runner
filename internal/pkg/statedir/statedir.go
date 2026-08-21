package statedir

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/usenorn/runner/internal/config"
)

const (
	dirMode = 0o700

	configFile   = "runner.yaml"
	identityFile = "identity.json"
	socketFile   = "runner.sock"
	lockFile     = "runner.lock"
	logFile      = "runner.log"

	workspacesDir = "workspaces"
	runsDir       = "runs"
	cacheDir      = "cache"
	spoolDir      = "spool"
	logsDir       = "logs"
)

type Dir struct {
	root string
}

func New(cfg config.State) (*Dir, error) {
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve state directory %q: %w", cfg.Root, err)
	}

	dir := &Dir{root: root}

	if err := os.MkdirAll(root, dirMode); err != nil {
		return nil, fmt.Errorf("create state directory %q: %w", root, err)
	}

	if err := os.Chmod(root, dirMode); err != nil {
		return nil, fmt.Errorf("restrict state directory %q: %w", root, err)
	}

	for _, child := range []string{dir.Workspaces(), dir.Runs(), dir.Cache(), dir.Spool(), dir.Logs()} {
		if err := os.MkdirAll(child, dirMode); err != nil {
			return nil, fmt.Errorf("create state directory %q: %w", child, err)
		}
	}

	return dir, nil
}

func (d *Dir) Root() string { return d.root }

func (d *Dir) Config() string { return filepath.Join(d.root, configFile) }

func (d *Dir) Identity() string { return filepath.Join(d.root, identityFile) }

func (d *Dir) Socket() string { return filepath.Join(d.root, socketFile) }

func (d *Dir) Lock() string { return filepath.Join(d.root, lockFile) }

func (d *Dir) Workspaces() string { return filepath.Join(d.root, workspacesDir) }

func (d *Dir) Runs() string { return filepath.Join(d.root, runsDir) }

func (d *Dir) Cache() string { return filepath.Join(d.root, cacheDir) }

func (d *Dir) Spool() string { return filepath.Join(d.root, spoolDir) }

func (d *Dir) Logs() string { return filepath.Join(d.root, logsDir) }

func (d *Dir) LogFile() string { return filepath.Join(d.Logs(), logFile) }

func (d *Dir) Enrolled() bool {
	_, err := os.Stat(d.Identity())

	return err == nil
}
