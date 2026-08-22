package service

import (
	"context"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=executions.go -destination=execution/mock_executions.go -package=execution -mock_names=Executions=MockExecutions

type Executions interface {
	Run(ctx context.Context)
	Offer(ctx context.Context, offer channelv1.Offer) error
	Start(ctx context.Context, executionID string, start channelv1.Start) error
	Cancel(ctx context.Context, executionID string, reason string) error
	Continue(ctx context.Context, executionID string, instruction channelv1.Instruction) error
	Reconcile(ctx context.Context, leased []string) error
	Pause()
	Resume()
	Configure(configuration channelv1.Configuration)
	Greeting() channelv1.Hello
	Pulse(ctx context.Context) channelv1.Pulse
	Report(ctx context.Context) entity.SchedulerReport
	Driver(ctx context.Context) entity.DriverHealth
	List(ctx context.Context) ([]entity.Execution, error)
	Timeline(ctx context.Context, executionID string) ([]entity.TimelineEntry, error)
}
