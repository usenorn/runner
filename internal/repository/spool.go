package repository

import (
	"context"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

//go:generate go tool mockgen -source=spool.go -destination=spool/mock_spool.go -package=spool -mock_names=Spool=MockSpool

type Spool interface {
	Append(ctx context.Context, message channelv1.Message) error
	Head(ctx context.Context, limit int) ([]channelv1.Message, error)
	Acknowledge(ctx context.Context, id string) error
	Prune(ctx context.Context, before time.Time, keep int) (int, error)
	Count(ctx context.Context) (int, error)
}
