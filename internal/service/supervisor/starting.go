package supervisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

func (s *servicesSupervisor) Start(
	ctx context.Context,
	executionID string,
	wanted entity.Service,
) (entity.ServiceRecord, error) {
	if err := wanted.Valid(); err != nil {
		return entity.ServiceRecord{}, err
	}

	execution, err := s.claim(ctx, executionID)
	if err != nil {
		return entity.ServiceRecord{}, err
	}

	ports, err := s.reserve(ctx, executionID, wanted)
	if err != nil {
		return entity.ServiceRecord{}, err
	}

	resolved, err := resolve(wanted, ports)
	if err != nil {
		return entity.ServiceRecord{}, err
	}

	entry, taken, err := s.take(execution, wanted, resolved, ports)
	if err != nil {
		return entity.ServiceRecord{}, err
	}

	if !taken {
		return s.snapshot(entry), nil
	}

	listening, err := s.spawn(ctx, execution, entry)
	if err != nil {
		close(entry.done)

		s.settle(ctx, executionID, entry, entity.ServiceUnhealthy, err.Error())

		return entity.ServiceRecord{}, err
	}

	go s.watch(context.WithoutCancel(ctx), execution, entry, listening)

	return s.snapshot(entry), nil
}

func (s *servicesSupervisor) snapshot(entry *supervised) entity.ServiceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	return entry.record
}

func (s *servicesSupervisor) claim(
	ctx context.Context,
	executionID string,
) (entity.Execution, error) {
	execution, err := s.runs.LoadTask(ctx, executionID)
	if err != nil {
		return entity.Execution{}, err
	}

	if execution.Finished() {
		return entity.Execution{}, fmt.Errorf(
			"%w: %s has already finished as %s", entity.ErrExecutionRefused, executionID,
			execution.State,
		)
	}

	return execution, nil
}

func (s *servicesSupervisor) reserve(
	ctx context.Context,
	executionID string,
	wanted entity.Service,
) (map[string]int, error) {
	for _, name := range wanted.Ports() {
		if _, err := s.ports.Reserve(ctx, executionID, name); err != nil {
			return nil, err
		}
	}

	return s.ports.Held(ctx, executionID)
}

func resolve(wanted entity.Service, ports map[string]int) (entity.Service, error) {
	resolved := wanted
	resolved.Command = make([]string, 0, len(wanted.Command))

	for _, argument := range wanted.Command {
		filled, err := entity.ResolvePorts(argument, ports)
		if err != nil {
			return entity.Service{}, err
		}

		resolved.Command = append(resolved.Command, filled)
	}

	resolved.Environment = map[string]string{}

	for key, value := range wanted.Environment {
		filled, err := entity.ResolvePorts(value, ports)
		if err != nil {
			return entity.Service{}, err
		}

		resolved.Environment[key] = filled
	}

	dir, err := entity.ResolvePorts(wanted.Dir, ports)
	if err != nil {
		return entity.Service{}, err
	}

	resolved.Dir = dir

	port, err := entity.ResolvePorts(wanted.Health.Port, ports)
	if err != nil {
		return entity.Service{}, err
	}

	resolved.Health.Port = port

	return resolved, nil
}

func (s *servicesSupervisor) take(
	execution entity.Execution,
	wanted entity.Service,
	resolved entity.Service,
	ports map[string]int,
) (*supervised, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	holding, known := s.held[execution.ID]
	if !known {
		holding = &run{execution: execution, services: map[string]*supervised{}}
		s.held[execution.ID] = holding
	}

	already, running := holding.services[wanted.Name]

	if running && attending(already) {
		return already, false, nil
	}

	for _, needed := range wanted.Requires {
		got, running := holding.services[needed]
		if !running || got.record.State != entity.ServiceHealthy {
			return &supervised{}, false, fmt.Errorf(
				"%w: %s needs %s, which is %s",
				entity.ErrServiceWaiting, wanted.Name, needed, standing(got, running),
			)
		}
	}

	entry := &supervised{
		wanted: resolved,
		ports:  ports,
		stream: carried(already, running),
		record: entity.ServiceRecord{
			Name:      wanted.Name,
			Command:   resolved.Command,
			Dir:       resolved.Dir,
			Port:      ports[wanted.Name],
			State:     entity.ServiceStarting,
			StartedAt: s.now(),
			ChangedAt: s.now(),
		},
		done: make(chan struct{}),
	}

	holding.services[wanted.Name] = entry

	return entry, true, nil
}

func carried(already *supervised, running bool) *stream {
	if !running {
		return nil
	}

	return already.stream
}

func standing(got *supervised, running bool) string {
	if !running {
		return "not running"
	}

	return string(got.record.State)
}

func (s *servicesSupervisor) spawn(
	ctx context.Context,
	execution entity.Execution,
	entry *supervised,
) (*heard, error) {
	s.mu.Lock()

	wanted, ports := entry.wanted, entry.ports

	s.mu.Unlock()

	if entry.stream == nil {
		sink, err := s.logs.Open(ctx, execution.ID, wanted.Name)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		entry.stream = newStream(sink)
		s.mu.Unlock()
	}

	lines, forget := entry.stream.Watch()

	child, err := s.processes.Start(ctx, repository.Launch{
		Dir:         filepath.Join(execution.Directory, entity.RunWorkspaceDir, wanted.Dir),
		Command:     wanted.Command,
		Environment: environment(execution, wanted, ports),
		Output:      entry.stream,
	})
	if err != nil {
		forget()

		return nil, err
	}

	s.mu.Lock()

	entry.child = child
	entry.record.PID = child.PID()
	entry.record.State = entity.ServiceStarting
	entry.record.Reason = fmt.Sprintf("it was started as process %d", child.PID())
	entry.record.StartedAt = s.now()
	entry.record.ChangedAt = s.now()

	s.mu.Unlock()

	s.persist(ctx, execution.ID)
	s.tell(ctx, execution.ID, said(wanted.Name, entity.ServiceStarting, entry.record.Reason))

	return &heard{lines: lines, forget: forget, child: child}, nil
}

type heard struct {
	lines  <-chan string
	forget func()
	child  repository.Child
}

func environment(
	execution entity.Execution,
	wanted entity.Service,
	ports map[string]int,
) []string {
	values := slices.Clone(os.Environ())

	values = append(values, "NORN_EXEC_ID="+execution.ID)

	for name, port := range ports {
		values = append(values, fmt.Sprintf("%s=%d", entity.PortVariable(name), port))
	}

	for key, value := range wanted.Environment {
		values = append(values, key+"="+value)
	}

	return values
}

func (s *servicesSupervisor) Stop(
	ctx context.Context,
	executionID string,
	name string,
) (entity.ServiceRecord, error) {
	entry, err := s.find(ctx, executionID, name)
	if err != nil {
		return entity.ServiceRecord{}, err
	}

	s.mu.Lock()

	if !attending(entry) {
		record := entry.record

		s.mu.Unlock()

		return record, nil
	}

	entry.stopping = true
	entry.because = stoppedNote

	s.mu.Unlock()

	s.settleDown(ctx, executionID, entry)

	s.mu.Lock()
	defer s.mu.Unlock()

	return entry.record, nil
}

func (s *servicesSupervisor) Restart(
	ctx context.Context,
	executionID string,
	name string,
) (entity.ServiceRecord, error) {
	entry, err := s.find(ctx, executionID, name)
	if err != nil {
		return entity.ServiceRecord{}, err
	}

	s.mu.Lock()
	wanted := entry.wanted
	s.mu.Unlock()

	if _, err := s.Stop(ctx, executionID, name); err != nil {
		return entity.ServiceRecord{}, err
	}

	return s.Start(ctx, executionID, wanted)
}

func (s *servicesSupervisor) find(
	ctx context.Context,
	executionID string,
	name string,
) (*supervised, error) {
	if _, err := s.runs.LoadTask(ctx, executionID); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	holding, known := s.held[executionID]
	if !known {
		return nil, fmt.Errorf("%w: %s", entity.ErrServiceUnknown, name)
	}

	entry, running := holding.services[name]
	if !running {
		return nil, fmt.Errorf("%w: %s", entity.ErrServiceUnknown, name)
	}

	return entry, nil
}
