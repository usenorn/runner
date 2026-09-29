package bridge

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"

	"github.com/usenorn/runner/internal/config"
)

const (
	containerHost = "host.docker.internal"
	dockerBridge  = "docker0"
)

var ErrUnavailable = errors.New("a container on this machine has no way back to the runner")

type Listener struct {
	net.Listener

	reason error
}

func New(cfg config.Docker) (*Listener, func(), error) {
	address, err := listenOn(cfg)
	if err != nil {
		return &Listener{reason: err}, func() {}, nil
	}

	inner, err := net.Listen("tcp", address)
	if err != nil {
		return &Listener{reason: fmt.Errorf("%w: listen on %s: %w", ErrUnavailable, address, err)}, func() {}, nil
	}

	return &Listener{Listener: inner}, func() { _ = inner.Close() }, nil
}

func listenOn(cfg config.Docker) (string, error) {
	if cfg.Bridge != "" {
		return cfg.Bridge, nil
	}

	if runtime.GOOS != "linux" {
		return "127.0.0.1:0", nil
	}

	gateway, err := net.InterfaceByName(dockerBridge)
	if err != nil {
		return "", fmt.Errorf("%w: there is no %s interface to listen on; set docker.bridge", ErrUnavailable, dockerBridge)
	}

	addresses, err := gateway.Addrs()
	if err != nil {
		return "", fmt.Errorf("%w: read the addresses of %s: %w", ErrUnavailable, dockerBridge, err)
	}

	for _, address := range addresses {
		if prefix, ok := address.(*net.IPNet); ok && prefix.IP.To4() != nil {
			return net.JoinHostPort(prefix.IP.String(), "0"), nil
		}
	}

	return "", fmt.Errorf("%w: %s has no IPv4 address; set docker.bridge", ErrUnavailable, dockerBridge)
}

func (l *Listener) Available() error {
	return l.reason
}

func (l *Listener) Reach() (string, error) {
	if l.reason != nil {
		return "", l.reason
	}

	address, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("%w: it listens on %s", ErrUnavailable, l.Addr())
	}

	return "http://" + net.JoinHostPort(containerHost, strconv.Itoa(address.Port)), nil
}
