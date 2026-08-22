package repository

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=capability.go -destination=capability/mock_capability.go -package=capability -mock_names=Capability=MockCapability

type Capabilities struct {
	Runtimes []entity.Runtime
	Tools    []entity.Tool
}

type Capability interface {
	Detect(ctx context.Context) Capabilities
}
