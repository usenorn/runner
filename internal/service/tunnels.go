package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=tunnels.go -destination=tunnel/mock_tunnels.go -package=tunnel -mock_names=Tunnels=MockTunnels

type Tunnels interface {
	Run(ctx context.Context)
	Wake()
	Report() entity.TunnelReport
}
