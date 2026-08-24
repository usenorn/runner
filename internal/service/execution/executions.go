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

const interruptedNote = "this machine restarted while the run was under way, so the work it had " +
	"started was left unfinished"

const waitingToPrepare = 64

type executionsService struct {
	runs        repository.Run
	spool       repository.Spool
	disks       repository.Disk
	scheduling  repository.Scheduling
	settings    repository.Settings
	inventories repository.Inventory
	snapshots   service.Snapshots
	services    service.Services
	uploads     service.Uploads
	questions   service.Questions
	previews    service.Previews
	serving     service.Sessions
	changesets  service.ChangeSets
	tokens      repository.RunToken
	drivers     repository.Driver
	dir         *statedir.Dir
	runner      config.Runner
	app         config.App
	scheduler   config.Scheduler
	driver      config.Driver
	now         func() time.Time

	preparing chan string
	resuming  chan resumption

	mu       sync.Mutex
	held     map[string]entity.Execution
	work     map[string]context.CancelFunc
	owed     map[string]bool
	done     map[string]entity.Completion
	commits  map[string]bool
	capacity int
	paused   bool
	usage    entity.RunsReport
}

func New(
	runs repository.Run,
	spool repository.Spool,
	disks repository.Disk,
	scheduling repository.Scheduling,
	settings repository.Settings,
	inventories repository.Inventory,
	snapshots service.Snapshots,
	services service.Services,
	uploads service.Uploads,
	questions service.Questions,
	previews service.Previews,
	serving service.Sessions,
	changesets service.ChangeSets,
	tokens repository.RunToken,
	drivers repository.Driver,
	dir *statedir.Dir,
	runner config.Runner,
	app config.App,
	scheduler config.Scheduler,
	driver config.Driver,
) service.Executions {
	return &executionsService{
		runs:        runs,
		spool:       spool,
		disks:       disks,
		scheduling:  scheduling,
		settings:    settings,
		inventories: inventories,
		snapshots:   snapshots,
		services:    services,
		uploads:     uploads,
		questions:   questions,
		previews:    previews,
		serving:     serving,
		changesets:  changesets,
		tokens:      tokens,
		drivers:     drivers,
		dir:         dir,
		runner:      runner,
		app:         app,
		scheduler:   scheduler,
		driver:      driver,
		now:         func() time.Time { return time.Now().UTC() },
		preparing:   make(chan string, waitingToPrepare),
		resuming:    make(chan resumption, waitingToPrepare),
		held:        map[string]entity.Execution{},
		work:        map[string]context.CancelFunc{},
		owed:        map[string]bool{},
		done:        map[string]entity.Completion{},
		commits:     map[string]bool{},
		capacity:    runner.Capacity,
	}
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
		Code:   string(reason),
		Detail: reason.Because(report),
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

	if _, err := s.runs.Open(ctx, execution.ID); err != nil {
		return s.fail(ctx, execution, entity.Failure(entity.StepRecord, err))
	}

	if start.LeaseExpiresAt != nil {
		execution.Lease = start.LeaseExpiresAt.UTC()
	}

	execution.StartedAt = s.now()

	if err := s.move(ctx, execution, channelv1.StatePreparing, ""); err != nil {
		return err
	}

	select {
	case s.preparing <- execution.ID:
	default:
		return s.fail(ctx, execution, "this machine has more runs waiting to be prepared than it can hold")
	}

	return nil
}

func (s *executionsService) Cancel(ctx context.Context, executionID, reason string) error {
	s.mu.Lock()
	execution, holding := s.held[executionID]
	delete(s.held, executionID)
	underway := s.stop(executionID)
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

	s.settle(ctx, execution, channelv1.StateCancelled, reason)

	if underway {
		return nil
	}

	return s.finished(ctx, executionID)
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

		if err := s.teardown(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

func (s *executionsService) Pause(ctx context.Context) {
	s.standby(ctx, true)
}

func (s *executionsService) Resume(ctx context.Context) {
	s.standby(ctx, false)
}

func (s *executionsService) standby(ctx context.Context, paused bool) {
	s.mu.Lock()
	s.paused = paused
	s.mu.Unlock()

	if err := s.scheduling.Pause(context.WithoutCancel(ctx), paused); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not write down whether it is taking work, so a restart will "+
				"forget it",
			slog.Bool("paused", paused),
			slog.String("error", err.Error()),
		)
	}
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
		Runs:       s.usage,
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
	if held, err := s.runs.LoadTask(ctx, executionID); err == nil && held.Reported() {
		return nil
	}

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

	s.settle(ctx, execution, channelv1.StateFailed, reason)

	return s.send(ctx, channelv1.ExecutionStateReport, execution.ID, channelv1.Report{
		State:    string(channelv1.StateFailed),
		Reason:   reason,
		Occurred: s.now(),
	})
}

func (s *executionsService) settle(
	ctx context.Context,
	execution entity.Execution,
	state entity.ExecutionState,
	reason string,
) {
	if execution.ID == "" {
		return
	}

	execution.State = state

	if state.Terminal() {
		execution.SettledAt = s.now()
	}

	if err := s.runs.SaveTask(context.WithoutCancel(ctx), execution); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not write down how a run ended",
			slog.String("execution_id", execution.ID),
			slog.String("error", err.Error()),
		)
	}

	s.record(ctx, execution.ID, entity.TimelineEntry{
		Kind:     channelv1.EventTransition,
		State:    state,
		Reason:   reason,
		Occurred: s.now(),
	})

	s.kept(ctx, execution)
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

	s.mu.Lock()
	_, holding := s.held[execution.ID]
	s.mu.Unlock()

	if !holding {
		return fmt.Errorf("%w: %s", entity.ErrExecutionUnknown, execution.ID)
	}

	execution.State = state

	if state.Terminal() {
		execution.SettledAt = s.now()
	}

	if err := s.runs.SaveTask(ctx, execution); err != nil {
		return err
	}

	s.mu.Lock()
	s.held[execution.ID] = execution
	s.mu.Unlock()

	s.record(ctx, execution.ID, entity.TimelineEntry{
		Kind:     channelv1.EventTransition,
		State:    state,
		Reason:   reason,
		Occurred: s.now(),
	})

	s.kept(ctx, execution)

	return s.send(ctx, channelv1.ExecutionStateReport, execution.ID, channelv1.Report{
		State:    string(state),
		Reason:   reason,
		Occurred: s.now(),
	})
}

func (s *executionsService) kept(ctx context.Context, execution entity.Execution) {
	keepUntil := entity.GiveBackAt(execution, s.runner.Retention.WorkspaceAfterDone)

	if keepUntil.IsZero() {
		return
	}

	if err := s.send(
		context.WithoutCancel(ctx),
		channelv1.ExecutionRetention,
		execution.ID,
		channelv1.Retention{KeepUntil: keepUntil},
	); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not tell norn when it gives a run's workspace back",
			slog.String("execution_id", execution.ID),
			slog.String("error", err.Error()),
		)
	}
}

func (s *executionsService) note(
	ctx context.Context,
	executionID string,
	kind channelv1.EventKind,
	reason string,
) error {
	s.record(ctx, executionID, entity.TimelineEntry{
		Kind:     kind,
		Reason:   reason,
		Occurred: s.now(),
	})

	return s.send(ctx, channelv1.ExecutionEvent, executionID, channelv1.Entry{
		Kind:     string(kind),
		Reason:   reason,
		Occurred: s.now(),
	})
}

func (s *executionsService) record(
	ctx context.Context,
	executionID string,
	entry entity.TimelineEntry,
) {
	if err := s.runs.Append(context.WithoutCancel(ctx), executionID, entry); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not add a line to a run's own timeline",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)
	}
}

func (s *executionsService) teardown(ctx context.Context, executionID string) error {
	s.questions.Forget(executionID)
	s.previews.Release(context.WithoutCancel(ctx), executionID)
	s.tokens.Release(context.WithoutCancel(ctx), executionID)
	s.forget(executionID)

	if err := s.services.Release(context.WithoutCancel(ctx), executionID); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not stop what a run had running",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)
	}

	if err := s.snapshots.Release(context.WithoutCancel(ctx), executionID); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not give a run's workspace back",
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
