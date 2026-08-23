package execution

import (
	"context"
	"encoding/json"
	"fmt"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func (s *executionsService) Progress(
	ctx context.Context,
	executionID string,
	progress entity.Progress,
) error {
	if err := progress.Valid(); err != nil {
		return err
	}

	if err := s.working(ctx, executionID); err != nil {
		return err
	}

	detail, err := json.Marshal(map[string]any{
		"phase": progress.Phase, "percent": progress.Percent,
	})
	if err != nil {
		return fmt.Errorf("encode what a run said it is doing: %w", err)
	}

	s.record(ctx, executionID, entity.TimelineEntry{
		Kind:     channelv1.EventPhase,
		Reason:   progress.Line(),
		Occurred: s.now(),
	})

	return s.send(ctx, channelv1.ExecutionEvent, executionID, channelv1.Entry{
		Kind:     string(channelv1.EventPhase),
		Reason:   progress.Line(),
		Detail:   detail,
		Occurred: s.now(),
	})
}

func (s *executionsService) Complete(
	ctx context.Context,
	executionID string,
	completion entity.Completion,
) error {
	if err := completion.Valid(); err != nil {
		return err
	}

	if err := s.working(ctx, executionID); err != nil {
		return err
	}

	s.mu.Lock()
	s.done[executionID] = completion
	s.mu.Unlock()

	return s.note(ctx, executionID, channelv1.EventPhase, "the coding agent says it is done: "+
		completion.Line())
}

func (s *executionsService) working(ctx context.Context, executionID string) error {
	execution, err := s.runs.LoadTask(ctx, executionID)
	if err != nil {
		return err
	}

	if execution.Finished() {
		return fmt.Errorf(
			"%w: %s has already finished as %s",
			entity.ErrExecutionRefused, executionID, execution.State,
		)
	}

	return nil
}

func (s *executionsService) completion(executionID string) (entity.Completion, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	said, told := s.done[executionID]

	return said, told
}

func (s *executionsService) restarting(executionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.done, executionID)
	delete(s.commits, executionID)
}

func (s *executionsService) forget(executionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.done, executionID)
	delete(s.commits, executionID)
}
