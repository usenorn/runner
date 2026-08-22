package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=channels.go -destination=channel/mock_channels.go -package=channel -mock_names=Channels=MockChannels

type Channels interface {
	Run(ctx context.Context)
	Report(ctx context.Context) entity.ChannelReport
	Wake()
}
