package repository

import (
	"context"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=sandbox.go -destination=sandbox/mock_sandbox.go -package=sandbox -mock_names=Sandbox=MockSandbox

type Sandbox interface {
	Check(ctx context.Context, runtime entity.Runtime) error
	Open(ctx context.Context, spec entity.SandboxSpec) error
	Start(ctx context.Context, box entity.Sandbox, launch Launch) (Child, error)
	Run(ctx context.Context, box entity.Sandbox, launch Launch, timeout time.Duration) (int, error)
	Has(ctx context.Context, box entity.Sandbox, command string) bool
	Tools(box entity.Sandbox) (string, error)
	Close(ctx context.Context, box entity.Sandbox) error
	Sweep(ctx context.Context) error
}
