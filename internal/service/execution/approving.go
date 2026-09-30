package execution

import (
	"context"
	"errors"
	"log/slog"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
)

type publishJob struct {
	executionID string
	approval    *channelv1.Instruction
}

func (s *executionsService) approving(
	ctx context.Context,
	executionID string,
	instruction channelv1.Instruction,
) error {
	s.mu.Lock()
	execution := s.held[executionID]

	allowed := execution.State == channelv1.StateAwaitingReview ||
		(instruction.Reason == channelv1.ResumePublish && execution.State == channelv1.StateApproved)
	s.mu.Unlock()

	if !allowed || !s.startPublishing(ctx, publishJob{executionID: executionID, approval: &instruction}) {
		logging.From(ctx).InfoContext(
			ctx,
			"norn asked this machine to publish a run that is not waiting to be published",
			slog.String("execution_id", executionID),
			slog.String("state", string(execution.State)),
			slog.String("reason", instruction.Reason),
		)
	}

	return nil
}

func (s *executionsService) startPublishing(ctx context.Context, job publishJob) bool {
	s.mu.Lock()
	if s.publishing[job.executionID] {
		s.mu.Unlock()

		return false
	}

	s.publishing[job.executionID] = true
	s.mu.Unlock()

	s.enqueuePublish(ctx, job)

	return true
}

func (s *executionsService) enqueuePublish(ctx context.Context, job publishJob) {
	select {
	case s.publishes <- job:
	default:
		s.mu.Lock()
		delete(s.publishing, job.executionID)
		s.mu.Unlock()

		logging.From(ctx).WarnContext(
			ctx,
			"this machine has too many runs waiting to publish, so this one waits for a retry",
			slog.String("execution_id", job.executionID),
		)
	}
}

func (s *executionsService) publish(ctx context.Context, job publishJob) {
	defer func() {
		s.mu.Lock()
		delete(s.publishing, job.executionID)
		s.mu.Unlock()
	}()

	s.mu.Lock()
	execution, holding := s.held[job.executionID]
	s.mu.Unlock()

	if !holding {
		return
	}

	s.complain(ctx, execution.ID, s.publishApproved(ctx, execution, job.approval))
}

func (s *executionsService) publishApproved(
	ctx context.Context,
	execution entity.Execution,
	approval *channelv1.Instruction,
) error {
	review, err := s.runs.LoadReview(ctx, execution.ID)
	if err != nil && !errors.Is(err, entity.ErrReviewMissing) {
		return s.fail(ctx, execution, entity.Failure(entity.StepPublish, err))
	}

	if approval == nil {
		stored, err := s.runs.LoadApproval(ctx, execution.ID)
		if errors.Is(err, entity.ErrApprovalMissing) {
			return s.refuse(ctx, execution, review, err)
		}

		if err != nil {
			return s.fail(ctx, execution, entity.Failure(entity.StepPublish, err))
		}

		approval = &stored
	}

	live, err := s.changesets.Tips(ctx, review)
	if err != nil {
		return s.fail(ctx, execution, entity.Failure(entity.StepPublish, err))
	}

	if refusal := entity.ApprovalRefusal(review, *approval, live); refusal != nil {
		return s.refuse(ctx, execution, review, refusal)
	}

	if err := s.runs.SaveApproval(ctx, execution.ID, *approval); err != nil {
		return s.fail(ctx, execution, entity.Failure(entity.StepPublish, err))
	}

	if execution.State != channelv1.StateApproved {
		execution.State = channelv1.StateApproved
		execution.Stage = channelv1.StagePublication

		if err := s.keep(ctx, execution); err != nil {
			return err
		}
	}

	publication, err := s.changesets.Publish(ctx, execution, review)
	if err != nil {
		return s.fail(ctx, execution, entity.Failure(entity.StepPublish, err))
	}

	if failed := publication.Failures(); len(failed) > 0 {
		return s.note(ctx, execution.ID, channelv1.EventNote, entity.PublicationIncomplete(failed))
	}

	return s.conclude(ctx, execution)
}

func (s *executionsService) refuse(
	ctx context.Context,
	execution entity.Execution,
	review entity.Review,
	refusal error,
) error {
	if execution.State != channelv1.StateApproved {
		execution.State = channelv1.StateApproved

		if err := s.keep(ctx, execution); err != nil {
			return err
		}
	}

	err := s.review(
		ctx, execution, entity.Completion{Summary: review.Summary},
		func(entity.ChangeSet) string { return entity.ApprovalRefused(refusal) },
	)

	var failed failure
	if errors.As(err, &failed) {
		return s.fail(ctx, execution, failed.Error())
	}

	return err
}

func (s *executionsService) abandon(ctx context.Context, executionID string) error {
	s.mu.Lock()
	execution, holding := s.held[executionID]
	claimed := holding && !s.publishing[executionID] && execution.State == channelv1.StateApproved

	if claimed {
		s.publishing[executionID] = true
	}
	s.mu.Unlock()

	if !claimed {
		logging.From(ctx).InfoContext(
			ctx,
			"norn asked this machine to give up on a publication it is not holding",
			slog.String("execution_id", executionID),
			slog.String("state", string(execution.State)),
		)

		return nil
	}

	defer func() {
		s.mu.Lock()
		delete(s.publishing, executionID)
		s.mu.Unlock()
	}()

	if err := s.move(ctx, execution, channelv1.StateFailed, entity.PublicationAbandoned()); err != nil {
		return err
	}

	return s.finished(ctx, executionID)
}

func (s *executionsService) keep(ctx context.Context, execution entity.Execution) error {
	if err := s.runs.SaveTask(ctx, execution); err != nil {
		return err
	}

	s.mu.Lock()
	s.held[execution.ID] = execution
	s.mu.Unlock()

	return nil
}

func (s *executionsService) conclude(ctx context.Context, execution entity.Execution) error {
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
