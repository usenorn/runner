package execution

import (
	"context"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func (s *executionsService) finalise(
	ctx context.Context,
	execution entity.Execution,
	completion entity.Completion,
) error {
	snapshot, err := s.runs.Load(ctx, execution.ID)
	if err != nil {
		return failure{step: entity.StepFinalise, err: err}
	}

	left, err := s.changesets.Uncommitted(ctx, snapshot)
	if err != nil {
		return failure{step: entity.StepFinalise, err: err}
	}

	if len(left) > 0 {
		return s.recommit(ctx, execution, left)
	}

	changes, err := s.changesets.Publish(
		ctx, execution, snapshot, completion, s.previewLinks(ctx, execution),
	)
	if err != nil {
		return failure{step: entity.StepFinalise, err: err}
	}

	if err := s.move(ctx, execution, channelv1.StateAwaitingReview, entity.Finalised(changes)); err != nil {
		return err
	}

	execution.State = channelv1.StateAwaitingReview

	return nil
}

func (s *executionsService) recommit(
	ctx context.Context,
	execution entity.Execution,
	left []entity.UncommittedWork,
) error {
	if s.asked(execution.ID) {
		return failure{step: entity.StepFinalise, err: uncommitted{left: left}}
	}

	held, err := s.resumable(ctx, execution.ID)
	if err != nil {
		return err
	}

	s.asking(execution.ID)

	if err := s.move(
		ctx, execution, channelv1.StateRunning, entity.CommitAsked(left),
	); err != nil {
		return err
	}

	execution.State = channelv1.StateRunning

	return s.again(ctx, execution, held, entity.CommitInjection(left))
}

type uncommitted struct {
	left []entity.UncommittedWork
}

func (u uncommitted) Error() string {
	return entity.UncommittedReason(u.left)
}

func (u uncommitted) Unwrap() error {
	return entity.ErrWorkUncommitted
}

func (s *executionsService) asked(executionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.commits[executionID]
}

func (s *executionsService) asking(executionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.commits[executionID] = true
}

func (s *executionsService) previewLinks(
	ctx context.Context,
	execution entity.Execution,
) []entity.PreviewLink {
	open, err := s.previews.List(ctx, execution.ID)
	if err != nil || len(open) == 0 {
		return nil
	}

	serving := s.serving.Previews()

	links := make([]entity.PreviewLink, 0, len(open))

	for _, preview := range open {
		address := serving.Address(execution, preview.Port, preview.Path)
		if address == "" {
			continue
		}

		links = append(links, entity.PreviewLink{Name: preview.Name, Address: address})
	}

	return links
}
