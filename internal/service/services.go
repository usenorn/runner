package service

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

//go:generate go tool mockgen -source=services.go -destination=supervisor/mock_services.go -package=supervisor -mock_names=Services=MockServices

type Services interface {
	Run(ctx context.Context)
	Start(
		ctx context.Context,
		executionID string,
		wanted entity.Service,
	) (entity.ServiceRecord, error)
	Stop(ctx context.Context, executionID string, name string) (entity.ServiceRecord, error)
	Restart(ctx context.Context, executionID string, name string) (entity.ServiceRecord, error)
	List(ctx context.Context, executionID string) ([]entity.ServiceRecord, error)
	Logs(
		ctx context.Context,
		executionID string,
		name string,
		query entity.LogQuery,
	) ([]string, error)
	Step(ctx context.Context, executionID string, step entity.Step) (entity.StepResult, error)
	Port(ctx context.Context, executionID string, name string) (int, error)
	Release(ctx context.Context, executionID string) error
}
