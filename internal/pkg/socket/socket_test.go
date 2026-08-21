package socket_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

func stateDir(t *testing.T) *statedir.Dir {
	t.Helper()

	root, err := os.MkdirTemp("/tmp", "nrn")
	if err != nil {
		t.Fatalf("create temporary root: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	return dir
}

func TestTheSocketIsReachableOnlyByItsOwner(t *testing.T) {
	dir := stateDir(t)

	listener, cleanup, err := socket.New(dir)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer cleanup()

	info, err := os.Stat(listener.Path())
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket is mode %o, want 600", perm)
	}

	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%s is not a socket", listener.Path())
	}
}

func TestASecondDaemonIsRefusedWhileTheFirstHoldsTheLock(t *testing.T) {
	dir := stateDir(t)

	_, cleanup, err := socket.New(dir)
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	defer cleanup()

	_, second, err := socket.New(dir)
	if err == nil {
		second()

		t.Fatalf("a second daemon bound the same state directory; both would fight over it")
	}

	if !strings.Contains(err.Error(), "already using") {
		t.Fatalf("refusal said %q, want it to name the conflict plainly", err)
	}
}

func TestASocketLeftBehindByACrashedDaemonIsReplaced(t *testing.T) {
	dir := stateDir(t)

	if err := os.WriteFile(dir.Socket(), []byte("debris"), 0o600); err != nil {
		t.Fatalf("plant debris: %v", err)
	}

	listener, cleanup, err := socket.New(dir)
	if err != nil {
		t.Fatalf("listen over debris: %v", err)
	}
	defer cleanup()

	info, err := os.Stat(listener.Path())
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}

	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("the leftover file was not replaced by a socket")
	}
}

func TestClosingTheListenerReleasesTheLockAndRemovesTheSocket(t *testing.T) {
	dir := stateDir(t)

	listener, cleanup, err := socket.New(dir)
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}

	path := listener.Path()

	cleanup()

	if _, err := os.Stat(path); err == nil {
		t.Fatalf("the socket outlived the daemon that owned it")
	}

	_, second, err := socket.New(dir)
	if err != nil {
		t.Fatalf("a later daemon could not take over: %v", err)
	}

	second()
}

func TestASocketPathTooLongForTheKernelIsRefusedWithAnExplanation(t *testing.T) {
	deep := filepath.Join(t.TempDir(), strings.Repeat("nested/", 20))

	dir, err := statedir.New(config.State{Root: deep})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	_, cleanup, err := socket.New(dir)
	if err == nil {
		cleanup()

		t.Skipf("this system accepted a %d character socket path", len(dir.Socket()))
	}

	if !strings.Contains(err.Error(), "longer than a unix socket address") {
		t.Fatalf("refusal said %q, want it to name the path length rather than surface EINVAL", err)
	}
}
