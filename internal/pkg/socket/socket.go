package socket

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"syscall"

	"github.com/usenorn/runner/internal/pkg/statedir"
)

const (
	socketMode = 0o600
	lockMode   = 0o600

	shortestSunPath = 104
)

type Listener struct {
	net.Listener

	path  string
	lock  *os.File
	owner int
}

func (l *Listener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		unixConn, ok := conn.(*net.UnixConn)
		if !ok {
			_ = conn.Close()

			continue
		}

		peer, err := peerOwner(unixConn)
		if err != nil || peer != l.owner {
			_ = conn.Close()

			continue
		}

		return conn, nil
	}
}

func New(dir *statedir.Dir) (*Listener, func(), error) {
	lock, err := os.OpenFile(dir.Lock(), os.O_CREATE|os.O_RDWR, lockMode)
	if err != nil {
		return nil, nil, fmt.Errorf("open runner lock: %w", err)
	}

	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()

		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil, fmt.Errorf(
				"another runner daemon is already using %s; stop it before starting a second",
				dir.Root(),
			)
		}

		return nil, nil, fmt.Errorf("lock runner state: %w", err)
	}

	if err := os.Remove(dir.Socket()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		release(lock)

		return nil, nil, fmt.Errorf("remove stale socket %s: %w", dir.Socket(), err)
	}

	inner, err := net.Listen("unix", dir.Socket())
	if err != nil {
		release(lock)

		return nil, nil, listenError(dir.Socket(), err)
	}

	if err := os.Chmod(dir.Socket(), socketMode); err != nil {
		_ = inner.Close()
		release(lock)

		return nil, nil, fmt.Errorf("restrict socket %s: %w", dir.Socket(), err)
	}

	listener := &Listener{Listener: inner, path: dir.Socket(), lock: lock, owner: os.Getuid()}

	return listener, listener.close, nil
}

func (l *Listener) Path() string {
	return l.path
}

func (l *Listener) close() {
	_ = l.Close()
	_ = os.Remove(l.path)

	release(l.lock)
}

func release(lock *os.File) {
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func listenError(path string, err error) error {
	if errors.Is(err, syscall.EINVAL) && len(path) > shortestSunPath {
		return fmt.Errorf(
			"the socket path %s is %d characters, which is longer than a unix socket address may "+
				"be on this system; point NORN_STATE_ROOT at a shorter directory",
			path, len(path),
		)
	}

	return fmt.Errorf("listen on %s: %w", path, err)
}
