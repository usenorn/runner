package socket

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func peerOwner(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("reach the connection's file descriptor: %w", err)
	}

	var (
		owner   int
		peerErr error
	)

	if err := raw.Control(func(fd uintptr) {
		credentials, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			peerErr = fmt.Errorf("read the peer's credentials: %w", err)

			return
		}

		owner = int(credentials.Uid)
	}); err != nil {
		return 0, fmt.Errorf("inspect the connection: %w", err)
	}

	return owner, peerErr
}
