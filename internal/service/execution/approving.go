package execution

import (
	"context"
	"errors"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func (s *executionsService) approve(ctx context.Context, execution entity.Execution) error {
	execution.State = channelv1.StateApproved

	if err := s.runs.SaveTask(ctx, execution); err != nil {
		return err
	}

	return s.publish(ctx, execution)
}

func (s *executionsService) publish(ctx context.Context, execution entity.Execution) error {
	snapshot, err := s.runs.Load(ctx, execution.ID)
	if err != nil {
		return s.fail(ctx, execution, entity.Failure(entity.StepPublish, err))
	}

	if _, err := s.changesets.Publish(ctx, execution, snapshot); err != nil {
		return s.fail(ctx, execution, entity.Failure(entity.StepPublish, err))
	}

	return s.conclude(ctx, execution)
}

func (s *executionsService) conclude(ctx context.Context, execution entity.Execution) error {
	s.mu.Lock()
	s.held[execution.ID] = execution
	s.mu.Unlock()

	if err := s.move(ctx, execution, channelv1.StateCompleted, entity.Approved()); err != nil {
		return err
	}

	return s.finished(ctx, execution.ID)
}

func (s *executionsService) finished(ctx context.Context, executionID string) error {
	s.mu.Lock()
	delete(s.held, executionID)
	s.mu.Unlock()

	s.complain(ctx, executionID, s.questions.Forget(context.WithoutCancel(ctx), executionID))
	s.tokens.Release(context.WithoutCancel(ctx), executionID)
	s.forget(executionID)

	if _, err := s.runs.Load(ctx, executionID); err != nil {
		if !errors.Is(err, entity.ErrSnapshotMissing) {
			return err
		}

		return s.teardown(ctx, executionID)
	}

	return s.note(ctx, executionID, channelv1.EventPhase, entity.Keeping(
		s.runner.Retention.WorkspaceAfterDone,
	))
}
