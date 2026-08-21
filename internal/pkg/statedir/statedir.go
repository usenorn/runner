package statedir

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/usenorn/runner/internal/config"
)

const (
	dirMode  = 0o700
	fileMode = 0o600

	configFile   = "runner.yaml"
	identityFile = "identity.json"
	secretsFile  = "credentials.enc"
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

func (d *Dir) Credentials() string { return filepath.Join(d.root, secretsFile) }

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

func WriteSecret(path string, raw []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create a temporary file beside %s: %w", path, err)
	}

	defer func() { _ = os.Remove(temp.Name()) }()

	if err := temp.Chmod(fileMode); err != nil {
		_ = temp.Close()

		return fmt.Errorf("restrict %s: %w", temp.Name(), err)
	}

	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()

		return fmt.Errorf("write %s: %w", temp.Name(), err)
	}

	if err := temp.Sync(); err != nil {
		_ = temp.Close()

		return fmt.Errorf("flush %s: %w", temp.Name(), err)
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", temp.Name(), err)
	}

	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("move %s into place at %s: %w", temp.Name(), path, err)
	}

	return nil
}
