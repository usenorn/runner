package execution

import (
	"context"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

type admission struct {
	executionID string
	resume      *resumption
}

func (s *executionsService) admit(ctx context.Context, next admission) {
	s.mu.Lock()
	s.waiting = append(s.waiting, next)
	s.mu.Unlock()

	s.dispatch(ctx)
}

func (s *executionsService) vacate(ctx context.Context, executionID string) {
	s.mu.Lock()
	delete(s.admitted, executionID)
	s.mu.Unlock()

	s.dispatch(ctx)
}

func (s *executionsService) dispatch(ctx context.Context) {
	s.mu.Lock()

	ready := make([]admission, 0, len(s.waiting))
	remaining := s.waiting[:0]
	busy := s.busy()

	for _, next := range s.waiting {
		if _, holding := s.held[next.executionID]; !holding {
			continue
		}

		if busy >= s.capacity {
			remaining = append(remaining, next)

			continue
		}

		s.admitted[next.executionID] = true
		busy++
		ready = append(ready, next)
	}

	s.waiting = remaining
	s.mu.Unlock()

	for _, next := range ready {
		s.launch(ctx, next)
	}
}

func (s *executionsService) busy() int {
	working := 0

	for executionID := range s.admitted {
		if _, holding := s.held[executionID]; holding {
			working++
		}
	}

	return working
}

func (s *executionsService) launch(ctx context.Context, next admission) {
	s.mu.Lock()
	execution, holding := s.held[next.executionID]
	s.mu.Unlock()

	if !holding {
		s.vacate(ctx, next.executionID)

		return
	}

	if next.resume != nil {
		select {
		case s.resuming <- *next.resume:
		default:
			s.overflow(ctx, execution)
		}

		return
	}

	if err := s.move(ctx, execution, channelv1.StatePreparing, ""); err != nil {
		s.complain(ctx, execution.ID, err)
		s.vacate(ctx, next.executionID)

		return
	}

	select {
	case s.preparing <- execution.ID:
	default:
		s.overflow(ctx, execution)
	}
}

func (s *executionsService) overflow(ctx context.Context, execution entity.Execution) {
	s.complain(ctx, execution.ID, s.fail(ctx, execution, overflowing))
	s.vacate(ctx, execution.ID)
}
