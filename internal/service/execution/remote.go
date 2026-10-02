package execution

import (
	"context"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func (s *executionsService) Refresh(ctx context.Context, executionID string) ([]entity.RemoteState, error) {
	if err := s.working(ctx, executionID); err != nil {
		return nil, err
	}

	snapshot, err := s.runs.Load(ctx, executionID)
	if err != nil {
		return nil, err
	}

	return s.fetched(ctx, executionID, snapshot), nil
}

func (s *executionsService) fetched(
	ctx context.Context,
	executionID string,
	snapshot entity.Snapshot,
) []entity.RemoteState {
	states := s.snapshots.Refresh(ctx, snapshot)

	for _, state := range states {
		s.complain(ctx, executionID, s.note(ctx, executionID, channelv1.EventNote, state.Line()))
	}

	return states
}
