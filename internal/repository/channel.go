package repository

import (
	"context"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

//go:generate go tool mockgen -source=channel.go -destination=channel/mock_channel.go -package=channel -mock_names=Channel=MockChannel,Conn=MockConn

type Conn interface {
	Read(ctx context.Context) (channelv1.Envelope, error)
	Write(ctx context.Context, envelope channelv1.Envelope) error
	Close() error
}

type Channel interface {
	Dial(ctx context.Context, ticket, version string) (Conn, error)
}
