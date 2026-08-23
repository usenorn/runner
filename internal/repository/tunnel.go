package repository

import (
	"context"
	"net"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=tunnel.go -destination=tunnel/mock_tunnel.go -package=tunnel -mock_names=Tunnel=MockTunnel

type TunnelSession interface {
	Accept() (net.Conn, error)
	Close() error
}

type Tunnel interface {
	Dial(ctx context.Context, ticket entity.TunnelTicket) (TunnelSession, error)
}
