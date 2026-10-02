package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=toolchains.go -destination=toolchain/mock_toolchains.go -package=toolchain -mock_names=Toolchains=MockToolchains

type Toolchains interface {
	Check(ctx context.Context, probe entity.ToolchainProbe) (entity.ToolchainReport, error)
	Doctor(ctx context.Context) (entity.Doctor, error)
}
