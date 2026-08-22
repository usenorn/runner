package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

const parkedNote = "this machine has taken the run and made its directory. Running a coding " +
	"agent is not built into this release, so the run stays here until it is cancelled"

type executionsService struct {
	runs      repository.Run
	spool     repository.Spool
	disks     repository.Disk
	dir       *statedir.Dir
	app       config.App
	scheduler config.Scheduler
	now       func() time.Time

	mu       sync.Mutex
	held     map[string]entity.Execution
	capacity int
	paused   bool
}

func New(
	runs repository.Run,
	spool repository.Spool,
	disks repository.Disk,
	dir *statedir.Dir,
	runner config.Runner,
	app config.App,
	scheduler config.Scheduler,
) service.Executions {
	return &executionsService{
		runs:      runs,
		spool:     spool,
		disks:     disks,
		dir:       dir,
		app:       app,
		scheduler: scheduler,
		now:       func() time.Time { return time.Now().UTC() },
		held:      map[string]entity.Execution{},
		capacity:  runner.Capacity,
	}
}

func (s *executionsService) Recover(ctx context.Context) error {
	found, err := s.runs.LoadTasks(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, execution := range found {
		if execution.Finished() {
			continue
		}

		s.held[execution.ID] = execution
	}

	return nil
}

func (s *executionsService) Offer(ctx context.Context, offer channelv1.Offer) error {
	if offer.ExecutionID == "" {
		return nil
	}

	report := s.Report(ctx)

	s.mu.Lock()

	if _, already := s.held[offer.ExecutionID]; already {
		s.mu.Unlock()

		return nil
	}

	reason, declining := report.Decline()
	if declining {
		s.mu.Unlock()

		return s.decline(ctx, offer.ExecutionID, reason, report)
	}

	execution := entity.ExecutionOf(offer, s.dir.Runs(), s.now())
	s.held[execution.ID] = execution

	s.mu.Unlock()

	return s.send(ctx, channelv1.ExecutionAccepted, execution.ID, struct{}{})
}

func (s *executionsService) decline(
	ctx context.Context,
	executionID string,
	reason entity.DeclineReason,
	report entity.SchedulerReport,
) error {
	logging.From(ctx).InfoContext(
		ctx,
		"this machine turned work down",
		slog.String("execution_id", executionID),
		slog.String("reason", string(reason)),
	)

	return s.send(ctx, channelv1.ExecutionDeclined, executionID, channelv1.Decline{
		Reason: fmt.Sprintf("%s: %s", reason, reason.Because(report)),
	})
}

func (s *executionsService) Start(
	ctx context.Context,
	executionID string,
	start channelv1.Start,
) error {
	s.mu.Lock()
	execution, holding := s.held[executionID]
	s.mu.Unlock()

	if !holding {
		return s.stranded(ctx, executionID)
	}

	if execution.State != channelv1.StateLeased {
		return nil
	}

	if _, err := s.runs.Prepare(ctx, execution.ID); err != nil {
		return s.fail(ctx, execution, err.Error())
	}

	if start.LeaseExpiresAt != nil {
		execution.Lease = start.LeaseExpiresAt.UTC()
	}

	execution.StartedAt = s.now()

	if err := s.move(ctx, execution, channelv1.StatePreparing, ""); err != nil {
		return err
	}

	return s.note(ctx, execution.ID, channelv1.EventPhase, parkedNote)
}

func (s *executionsService) Cancel(ctx context.Context, executionID, reason string) error {
	s.mu.Lock()
	_, holding := s.held[executionID]
	delete(s.held, executionID)
	s.mu.Unlock()

	if !holding {
		return nil
	}

	logging.From(ctx).InfoContext(
		ctx,
		"norn cancelled a run this machine was holding",
		slog.String("execution_id", executionID),
		slog.String("reason", reason),
	)

	return s.discard(ctx, executionID)
}

func (s *executionsService) Reconcile(ctx context.Context, leased []string) error {
	believed := make(map[string]bool, len(leased))

	for _, executionID := range leased {
		believed[executionID] = true
	}

	s.mu.Lock()
	held := make(map[string]entity.Execution, len(s.held))

	for id, execution := range s.held {
		held[id] = execution
	}
	s.mu.Unlock()

	for _, executionID := range leased {
		if _, holding := held[executionID]; holding {
			continue
		}

		if err := s.stranded(ctx, executionID); err != nil {
			return err
		}
	}

	for id := range held {
		if believed[id] {
			continue
		}

		s.mu.Lock()
		delete(s.held, id)
		s.mu.Unlock()

		logging.From(ctx).InfoContext(
			ctx,
			"norn no longer expects a run this machine still held, so it was cleared away",
			slog.String("execution_id", id),
		)

		if err := s.discard(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

func (s *executionsService) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.paused = true
}

func (s *executionsService) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.paused = false
}

func (s *executionsService) Configure(configuration channelv1.Configuration) {
	if configuration.Capacity == nil || *configuration.Capacity < 1 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.capacity = *configuration.Capacity
}

func (s *executionsService) Greeting() channelv1.Hello {
	s.mu.Lock()
	defer s.mu.Unlock()

	executions := make([]string, 0, len(s.held))

	for id := range s.held {
		executions = append(executions, id)
	}

	sort.Strings(executions)

	return channelv1.Hello{
		Version:    s.app.Version,
		Protocol:   entity.ChannelProtocol,
		Capacity:   s.capacity,
		Executions: executions,
	}
}

func (s *executionsService) Pulse(ctx context.Context) channelv1.Pulse {
	report := s.Report(ctx)

	phases := make([]channelv1.Phase, 0, len(report.Executions))

	for _, execution := range report.Executions {
		phases = append(phases, channelv1.Phase{
			ExecutionID: execution.ID,
			State:       string(execution.State),
		})
	}

	return channelv1.Pulse{
		Capacity:     report.Capacity,
		Used:         report.Used,
		Paused:       report.Paused,
		DiskPressure: report.Room.Pressed(),
		Phases:       phases,
	}
}

func (s *executionsService) Report(ctx context.Context) entity.SchedulerReport {
	s.mu.Lock()

	report := entity.SchedulerReport{
		Capacity:   s.capacity,
		Paused:     s.paused,
		Executions: make([]entity.Execution, 0, len(s.held)),
	}

	for _, execution := range s.held {
		report.Executions = append(report.Executions, execution)

		if execution.HoldsSlot() {
			report.Used++
		}
	}
	s.mu.Unlock()

	sort.Slice(report.Executions, func(i, j int) bool {
		return report.Executions[i].AcceptedAt.Before(report.Executions[j].AcceptedAt)
	})

	report.Room = s.room(ctx)

	return report
}

func (s *executionsService) room(ctx context.Context) entity.Room {
	room := entity.Room{Watermark: s.scheduler.MinFreeDisk}

	free, err := s.disks.Free(ctx, s.dir.Root())
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not tell how much room is left for a run",
			slog.String("error", err.Error()),
		)

		return room
	}

	room.Free = free
	room.Known = true

	return room
}

func (s *executionsService) stranded(ctx context.Context, executionID string) error {
	logging.From(ctx).WarnContext(
		ctx,
		"norn believes this machine holds a run it cannot find",
		slog.String("execution_id", executionID),
	)

	return s.send(ctx, channelv1.ExecutionStateReport, executionID, channelv1.Report{
		State:    string(channelv1.StateFailed),
		Reason:   "this machine no longer has the workspace for this run",
		Occurred: s.now(),
	})
}

func (s *executionsService) fail(
	ctx context.Context,
	execution entity.Execution,
	reason string,
) error {
	s.mu.Lock()
	delete(s.held, execution.ID)
	s.mu.Unlock()

	return s.send(ctx, channelv1.ExecutionStateReport, execution.ID, channelv1.Report{
		State:    string(channelv1.StateFailed),
		Reason:   reason,
		Occurred: s.now(),
	})
}

func (s *executionsService) move(
	ctx context.Context,
	execution entity.Execution,
	state entity.ExecutionState,
	reason string,
) error {
	if !execution.CanReport(state) {
		return fmt.Errorf("%w: %s to %s", entity.ErrExecutionRefused, execution.State, state)
	}

	execution.State = state

	if err := s.runs.SaveTask(ctx, execution); err != nil {
		return err
	}

	s.mu.Lock()
	s.held[execution.ID] = execution
	s.mu.Unlock()

	return s.send(ctx, channelv1.ExecutionStateReport, execution.ID, channelv1.Report{
		State:    string(state),
		Reason:   reason,
		Occurred: s.now(),
	})
}

func (s *executionsService) note(
	ctx context.Context,
	executionID string,
	kind channelv1.EventKind,
	reason string,
) error {
	return s.send(ctx, channelv1.ExecutionEvent, executionID, channelv1.Entry{
		Kind:     string(kind),
		Reason:   reason,
		Occurred: s.now(),
	})
}

func (s *executionsService) discard(ctx context.Context, executionID string) error {
	if err := s.runs.Remove(ctx, executionID); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not clear a run directory away",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

func (s *executionsService) send(
	ctx context.Context,
	kind channelv1.MessageType,
	executionID string,
	payload any,
) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode a %s: %w", kind, err)
	}

	message, err := channelv1.NewRunnerMessage(kind, executionID, raw, s.now())
	if err != nil {
		return err
	}

	return s.spool.Append(ctx, message)
}
