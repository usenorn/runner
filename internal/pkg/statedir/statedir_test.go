package statedir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

func TestTheStateDirectoryAndAllItsChildrenAreCreated(t *testing.T) {
	root := filepath.Join(t.TempDir(), "norn")

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	for _, path := range []string{dir.Codebases(), dir.Runs(), dir.Cache(), dir.Spool(), dir.Logs()} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}

		if !info.IsDir() {
			t.Fatalf("%s is not a directory", path)
		}
	}
}

func TestTheStateDirectoryIsReachableOnlyByItsOwner(t *testing.T) {
	root := filepath.Join(t.TempDir(), "norn")

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	for _, path := range []string{dir.Root(), dir.Runs(), dir.Logs()} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}

		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s is mode %o, want 700; the socket and the device key live under it", path, perm)
		}
	}
}

func TestAnExistingStateDirectoryLeftWideOpenIsTightened(t *testing.T) {
	root := filepath.Join(t.TempDir(), "norn")

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("pre-create: %v", err)
	}

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	info, err := os.Stat(dir.Root())
	if err != nil {
		t.Fatalf("stat root: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("a directory that already existed at 755 stayed %o", perm)
	}
}

func TestCreatingTheStateDirectoryTwiceIsNotAnError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "norn")

	if _, err := statedir.New(config.State{Root: root}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	if _, err := statedir.New(config.State{Root: root}); err != nil {
		t.Fatalf("second create: %v", err)
	}
}

func TestEveryNamedPathSitsInsideTheStateDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "norn")

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	paths := []string{
		dir.Config(), dir.Identity(), dir.Socket(), dir.Lock(),
		dir.Codebases(), dir.Runs(), dir.Cache(), dir.Spool(), dir.Logs(), dir.LogFile(),
	}

	for _, path := range paths {
		if !strings.HasPrefix(path, dir.Root()+string(os.PathSeparator)) {
			t.Fatalf("%s escapes the state directory %s", path, dir.Root())
		}
	}
}

func TestARunnerIsNotEnrolledUntilAnIdentityFileExists(t *testing.T) {
	dir, err := statedir.New(config.State{Root: filepath.Join(t.TempDir(), "norn")})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	if dir.Enrolled() {
		t.Fatalf("a fresh state directory reported itself enrolled")
	}

	if err := os.WriteFile(dir.Identity(), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	if !dir.Enrolled() {
		t.Fatalf("an identity file was written and the runner still reports itself unenrolled")
	}
}
