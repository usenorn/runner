package supervisor

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
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

const (
	stoppedNote  = "this run asked it to stop"
	drainingNote = "this machine is shutting down"
	strandedNote = "this machine restarted while it was running, so it was left behind"
	sweptNote    = "this machine restarted while it was running and has now stopped it"
)

type supervised struct {
	wanted entity.Service
	record entity.ServiceRecord
	child  repository.Child
	stream *stream
	ports  map[string]int

	stopping bool
	because  string
	stop     context.CancelFunc
	done     chan struct{}
}

type run struct {
	execution entity.Execution
	services  map[string]*supervised
}

type servicesSupervisor struct {
	processes repository.Process
	ports     repository.Port
	logs      repository.ServiceLog
	runs      repository.Run
	spool     repository.Spool
	cfg       config.Supervisor
	now       func() time.Time

	mu   sync.Mutex
	held map[string]*run
}

func New(
	processes repository.Process,
	ports repository.Port,
	logs repository.ServiceLog,
	runs repository.Run,
	spool repository.Spool,
	cfg config.Supervisor,
) service.Services {
	return &servicesSupervisor{
		processes: processes,
		ports:     ports,
		logs:      logs,
		runs:      runs,
		spool:     spool,
		cfg:       cfg,
		now:       func() time.Time { return time.Now().UTC() },
		held:      map[string]*run{},
	}
}

func (s *servicesSupervisor) Run(ctx context.Context) {
	s.reconcile(ctx)

	<-ctx.Done()

	s.drain(context.WithoutCancel(ctx))
}

func (s *servicesSupervisor) reconcile(ctx context.Context) {
	executions, err := s.runs.LoadTasks(ctx)
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not read what its runs were running",
			slog.String("error", err.Error()),
		)

		return
	}

	for _, execution := range executions {
		s.reclaim(ctx, execution)
	}
}

func (s *servicesSupervisor) reclaim(ctx context.Context, execution entity.Execution) {
	services, err := s.runs.LoadServices(ctx, execution.ID)
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not read a run's services",
			slog.String("execution_id", execution.ID),
			slog.String("error", err.Error()),
		)

		return
	}

	changed := false

	for at, record := range services.Services {
		if !record.State.Live() {
			continue
		}

		reason := strandedNote

		if s.processes.Stray(ctx, record.PID, record.StartedAt) {
			if err := s.processes.Sweep(ctx, record.PID, s.cfg.StopGrace); err != nil {
				logging.From(ctx).WarnContext(
					ctx,
					"this machine could not stop a service an earlier run left behind",
					slog.String("execution_id", execution.ID),
					slog.String("service", record.Name),
					slog.String("error", err.Error()),
				)
			}

			reason = sweptNote
		}

		services.Services[at].State = entity.ServiceStopped
		services.Services[at].Reason = reason
		services.Services[at].PID = 0
		services.Services[at].ChangedAt = s.now()

		changed = true

		s.tell(ctx, execution.ID, said(record.Name, entity.ServiceStopped, reason))
	}

	if !changed {
		return
	}

	if err := s.runs.SaveServices(ctx, execution.ID, services); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not write down what a run's services are doing",
			slog.String("execution_id", execution.ID),
			slog.String("error", err.Error()),
		)
	}
}

func (s *servicesSupervisor) drain(ctx context.Context) {
	s.mu.Lock()

	executions := make([]string, 0, len(s.held))

	for executionID := range s.held {
		executions = append(executions, executionID)
	}

	s.mu.Unlock()

	for _, executionID := range executions {
		s.quiet(ctx, executionID, drainingNote)
	}
}

func (s *servicesSupervisor) Release(ctx context.Context, executionID string) error {
	s.quiet(ctx, executionID, stoppedNote)

	s.mu.Lock()

	holding, known := s.held[executionID]

	delete(s.held, executionID)

	s.mu.Unlock()

	if known {
		for _, entry := range holding.services {
			_ = entry.stream.Close()
		}
	}

	s.ports.Release(ctx, executionID)

	return nil
}

func (s *servicesSupervisor) quiet(ctx context.Context, executionID string, reason string) {
	s.mu.Lock()

	holding, known := s.held[executionID]
	if !known {
		s.mu.Unlock()

		return
	}

	entries := make([]*supervised, 0, len(holding.services))

	for _, entry := range holding.services {
		if !attending(entry) {
			continue
		}

		entry.stopping = true
		entry.because = reason

		entries = append(entries, entry)
	}

	s.mu.Unlock()

	var stopping sync.WaitGroup

	for _, entry := range entries {
		stopping.Add(1)

		go func() {
			defer stopping.Done()

			s.settleDown(ctx, executionID, entry)
		}()
	}

	stopping.Wait()
}

func (s *servicesSupervisor) settleDown(ctx context.Context, executionID string, entry *supervised) {
	s.mu.Lock()

	stop, child := entry.stop, entry.child

	s.mu.Unlock()

	if stop != nil {
		stop()
	}

	if child != nil {
		if err := child.Stop(ctx, s.cfg.StopGrace); err != nil {
			logging.From(ctx).WarnContext(
				ctx,
				"this machine could not stop a service",
				slog.String("execution_id", executionID),
				slog.String("service", entry.record.Name),
				slog.String("error", err.Error()),
			)
		}
	}

	<-entry.done
}

func (s *servicesSupervisor) List(
	ctx context.Context,
	executionID string,
) ([]entity.ServiceRecord, error) {
	if _, err := s.runs.LoadTask(ctx, executionID); err != nil {
		return nil, err
	}

	s.mu.Lock()

	holding, known := s.held[executionID]

	if !known {
		s.mu.Unlock()

		services, err := s.runs.LoadServices(ctx, executionID)
		if err != nil {
			return nil, err
		}

		return services.Services, nil
	}

	records := make([]entity.ServiceRecord, 0, len(holding.services))

	for _, entry := range holding.services {
		records = append(records, entry.record)
	}

	s.mu.Unlock()

	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })

	return records, nil
}

func (s *servicesSupervisor) Logs(
	ctx context.Context,
	executionID string,
	name string,
	tail int,
) ([]string, error) {
	if _, err := s.runs.LoadTask(ctx, executionID); err != nil {
		return nil, err
	}

	s.mu.Lock()

	holding, known := s.held[executionID]

	var entry *supervised

	if known {
		entry = holding.services[name]
	}

	s.mu.Unlock()

	if entry != nil && entry.record.State.Live() {
		return entry.stream.Recent(tail), nil
	}

	return s.logs.Tail(ctx, executionID, name, tail)
}

func (s *servicesSupervisor) settle(
	ctx context.Context,
	executionID string,
	entry *supervised,
	state entity.ServiceState,
	reason string,
) {
	s.mu.Lock()

	if entry.record.State == state && entry.record.Reason == reason {
		s.mu.Unlock()

		return
	}

	entry.record.State = state
	entry.record.Reason = reason
	entry.record.ChangedAt = s.now()

	if state == entity.ServiceStopped {
		entry.record.PID = 0
	}

	s.mu.Unlock()

	s.persist(ctx, executionID)
	s.tell(ctx, executionID, said(entry.record.Name, state, reason))
}

func attending(entry *supervised) bool {
	select {
	case <-entry.done:
		return false
	default:
		return true
	}
}

func said(name string, state entity.ServiceState, reason string) string {
	return fmt.Sprintf("%s is %s: %s", name, state, reason)
}

func (s *servicesSupervisor) persist(ctx context.Context, executionID string) {
	ctx = context.WithoutCancel(ctx)

	services, err := s.runs.LoadServices(ctx, executionID)
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not read a run's services before writing them",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)

		return
	}

	ports, err := s.ports.Held(ctx, executionID)
	if err != nil {
		return
	}

	s.mu.Lock()

	holding, known := s.held[executionID]

	records := []entity.ServiceRecord{}

	if known {
		for _, entry := range holding.services {
			records = append(records, entry.record)
		}
	}

	s.mu.Unlock()

	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })

	services.Ports = ports
	services.Services = records

	if err := s.runs.SaveServices(ctx, executionID, services); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not write down what a run's services are doing",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)
	}
}

func (s *servicesSupervisor) tell(ctx context.Context, executionID string, reason string) {
	ctx = context.WithoutCancel(ctx)

	entry := entity.TimelineEntry{
		Kind:     channelv1.EventService,
		Reason:   reason,
		Occurred: s.now(),
	}

	if err := s.runs.Append(ctx, executionID, entry); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not add a line to a run's own timeline",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)
	}

	raw, err := json.Marshal(channelv1.Entry{
		Kind:     string(channelv1.EventService),
		Reason:   reason,
		Occurred: entry.Occurred,
	})
	if err != nil {
		return
	}

	message, err := channelv1.NewRunnerMessage(
		channelv1.ExecutionEvent, executionID, raw, s.now(),
	)
	if err != nil {
		return
	}

	if err := s.spool.Append(ctx, message); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not tell norn what a service is doing",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)
	}
}
