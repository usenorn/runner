package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/service"
)

type failure struct {
	step entity.PrepareStep
	err  error
}

func (f failure) Error() string {
	return entity.Failure(f.step, f.err)
}

func (f failure) Unwrap() error {
	return f.err
}

func (s *executionsService) Run(ctx context.Context) {
	s.standing(ctx)

	if err := s.reclaim(ctx); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not read back the runs it was holding",
			slog.String("error", err.Error()),
		)
	}

	var working sync.WaitGroup

	working.Add(1)

	go func() {
		defer working.Done()

		s.collect(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			working.Wait()

			return
		case executionID := <-s.preparing:
			working.Add(1)

			go func() {
				defer working.Done()

				s.prepare(ctx, executionID)
			}()
		case held := <-s.resuming:
			working.Add(1)

			go func() {
				defer working.Done()

				s.resume(ctx, held)
			}()
		}
	}
}

func (s *executionsService) standing(ctx context.Context) {
	paused, err := s.scheduling.Paused(ctx)
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not read back whether it was paused, so it is taking work",
			slog.String("error", err.Error()),
		)

		return
	}

	if !paused {
		return
	}

	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()

	logging.From(ctx).InfoContext(
		ctx,
		"this machine was paused when it last stopped and is still not taking work",
	)
}

func (s *executionsService) reclaim(ctx context.Context) error {
	found, err := s.runs.LoadTasks(ctx)
	if err != nil {
		return err
	}

	for _, execution := range found {
		if execution.Finished() {
			continue
		}

		if execution.State == channelv1.StateAwaitingReview {
			s.mu.Lock()
			s.held[execution.ID] = execution
			s.mu.Unlock()

			logging.From(ctx).InfoContext(
				ctx,
				"a run was waiting for somebody to review it when this machine last stopped",
				slog.String("execution_id", execution.ID),
			)

			continue
		}

		logging.From(ctx).InfoContext(
			ctx,
			"a run was still under way when this machine last stopped",
			slog.String("execution_id", execution.ID),
			slog.String("state", string(execution.State)),
		)

		if err := s.note(ctx, execution.ID, channelv1.EventPhase, interruptedNote); err != nil {
			return err
		}

		s.settle(ctx, execution, channelv1.StateInterrupted, interruptedNote)

		if err := s.teardown(ctx, execution.ID); err != nil {
			return err
		}
	}

	return nil
}

func (s *executionsService) prepare(base context.Context, executionID string) {
	ctx, stop := context.WithCancel(base)

	s.mu.Lock()
	execution, holding := s.held[executionID]

	if holding {
		s.work[executionID] = stop
	}
	s.mu.Unlock()

	if !holding {
		stop()

		return
	}

	err := s.carry(ctx, execution)

	s.mu.Lock()
	delete(s.work, executionID)
	owed := s.owed[executionID]
	delete(s.owed, executionID)
	s.mu.Unlock()

	stop()

	settled := context.WithoutCancel(base)

	switch {
	case owed:
		s.complain(settled, executionID, s.finished(settled, executionID))
	case base.Err() != nil:
		// The machine is stopping, not the run. It stays written down as it was, and the reclaim
		// on the way back in is what says it was interrupted; failing it here would settle a run
		// against a lease norn may still hold.
	case err != nil:
		s.complain(settled, executionID, s.fail(settled, execution, err.Error()))
		s.complain(settled, executionID, s.finished(settled, executionID))
	}
}

func (s *executionsService) complain(ctx context.Context, executionID string, err error) {
	if err == nil {
		return
	}

	logging.From(ctx).WarnContext(
		ctx,
		"this machine could not tell norn how a run got on",
		slog.String("execution_id", executionID),
		slog.String("error", err.Error()),
	)
}

func (s *executionsService) carry(ctx context.Context, execution entity.Execution) error {
	snapshot, setup, err := s.fill(ctx, execution)
	if err != nil {
		return err
	}

	return s.drive(ctx, execution, snapshot, setup)
}

func (s *executionsService) fill(
	ctx context.Context,
	execution entity.Execution,
) (entity.Snapshot, entity.RunSetup, error) {
	codebase, err := s.codebase(ctx)
	if err != nil {
		return entity.Snapshot{}, entity.RunSetup{}, failure{step: entity.StepCodebase, err: err}
	}

	if err := s.note(ctx, execution.ID, channelv1.EventPhase, fmt.Sprintf(
		"this run works in %s", codebase.RootPath,
	)); err != nil {
		return entity.Snapshot{}, entity.RunSetup{}, err
	}

	setup, err := s.setup(ctx, execution, codebase)
	if err != nil {
		return entity.Snapshot{}, entity.RunSetup{}, failure{step: entity.StepSetup, err: err}
	}

	if err := s.note(ctx, execution.ID, channelv1.EventPhase, told(setup)); err != nil {
		return entity.Snapshot{}, entity.RunSetup{}, err
	}

	health := s.drivers.Preflight(ctx, setup.Driver.Kind)

	if err := health.Fault(); err != nil {
		return entity.Snapshot{}, entity.RunSetup{}, failure{step: entity.StepDriver, err: err}
	}

	snapshot, err := s.snapshots.Take(ctx, service.TakeRequest{
		Path:         codebase.RootPath,
		IssueKey:     execution.IssueKey,
		Attempt:      execution.Attempt,
		Run:          execution.ID,
		LocalChanges: localChangesFor(execution),
		Base:         entity.BasePolicy(execution.BaseRef),
		Branch:       execution.Branch,
		Branches:     s.reused(ctx, execution),
	})
	if err != nil {
		return entity.Snapshot{}, entity.RunSetup{}, failure{step: entity.StepSnapshot, err: err}
	}

	for _, warning := range snapshot.Warnings {
		if err := s.note(ctx, execution.ID, channelv1.EventNote, warning); err != nil {
			return entity.Snapshot{}, entity.RunSetup{}, err
		}
	}

	if err := s.note(ctx, execution.ID, channelv1.EventPhase, ready(snapshot)); err != nil {
		return entity.Snapshot{}, entity.RunSetup{}, err
	}

	return snapshot, setup, nil
}

func (s *executionsService) codebase(ctx context.Context) (entity.Codebase, error) {
	connected, err := s.inventories.List(ctx)
	if err != nil {
		return entity.Codebase{}, err
	}

	switch len(connected) {
	case 0:
		return entity.Codebase{}, entity.ErrExecutionNoCodebase
	case 1:
		return connected[0], nil
	default:
		return entity.Codebase{}, fmt.Errorf(
			"%w, and it has %d", entity.ErrExecutionManyCodebases, len(connected),
		)
	}
}

func (s *executionsService) setup(
	ctx context.Context,
	execution entity.Execution,
	codebase entity.Codebase,
) (entity.RunSetup, error) {
	plan, err := s.plan(ctx, codebase.RootPath)
	if err != nil {
		return entity.RunSetup{}, err
	}

	setup := entity.RunSetup{
		Permissions: profileFor(execution, s.driver.Profile),
		Plan:        plan,
		Driver:      driverFor(execution, codebase),
		Services:    runtimeFor(execution, s.runner.Runtime),
	}

	return setup, s.runs.SaveSetup(ctx, execution.ID, setup)
}

func (s *executionsService) plan(ctx context.Context, root string) (entity.RunPlan, error) {
	path, err := s.settings.Plan(ctx, root)
	if err != nil {
		return entity.RunPlan{}, err
	}

	if path == "" {
		return entity.RunPlan{Source: entity.PlanNone}, nil
	}

	return entity.RunPlan{Source: entity.PlanCodebase, Path: path}, nil
}

func profileFor(execution entity.Execution, allowed config.Profile) entity.RunPermissions {
	ceiling := entity.PermissionProfile(allowed)
	if !ceiling.Valid() {
		ceiling = entity.ProfileStandard
	}

	asked := entity.PermissionProfile(execution.Profile)

	switch {
	case !asked.Valid():
		return entity.RunPermissions{
			Profile: ceiling,
			Chosen:  "the delegation named no profile, so this machine took its own default",
		}
	case asked.Exceeds(ceiling):
		return entity.RunPermissions{
			Profile: ceiling,
			Chosen: fmt.Sprintf(
				"the delegation asked for %s, and this machine goes no further than %s",
				asked, ceiling,
			),
		}
	default:
		return entity.RunPermissions{
			Profile: asked,
			Chosen:  "the delegation asked for it",
		}
	}
}

func localChangesFor(execution entity.Execution) entity.LocalChanges {
	if execution.IncludeDirty {
		return entity.LocalChangesInclude
	}

	return ""
}

func driverFor(execution entity.Execution, codebase entity.Codebase) entity.RunDriver {
	driver := entity.RunDriver{
		Kind:   entity.DriverKind(execution.Tool),
		Model:  execution.Model,
		Chosen: "the delegation asked for it",
	}

	if !driver.Kind.Valid() {
		driver.Kind = entity.DriverClaude
		driver.Chosen = "the delegation named no coding agent, so this machine took its default"
	}

	for _, tool := range codebase.Confirmed.Tools {
		if tool.Name == string(driver.Kind) {
			driver.Installed = true
			driver.Version = tool.Version

			break
		}
	}

	return driver
}

func runtimeFor(execution entity.Execution, asked config.Runtime) entity.RunServices {
	if named := entity.Runtime(execution.Runtime); named.Valid() {
		return entity.RunServices{Runtime: named, Chosen: "the delegation asked for it"}
	}

	if named := entity.Runtime(asked); named.Valid() {
		return entity.RunServices{
			Runtime: named, Chosen: "this machine's configuration asks for it",
		}
	}

	return entity.RunServices{
		Runtime: entity.RuntimeProcess,
		Chosen: "nothing asked for anything else, and this release cannot yet read a run plan " +
			"to know whether the services want docker",
	}
}

func told(setup entity.RunSetup) string {
	told := fmt.Sprintf(
		"this run is set up for %s under the %s profile on the %s runtime",
		setup.Driver.Kind, setup.Permissions.Profile, setup.Services.Runtime,
	)

	if !setup.Driver.Installed {
		told += fmt.Sprintf(", and %s is not on this machine", setup.Driver.Kind)
	}

	if setup.Plan.Source == entity.PlanNone {
		return told + ", with no run plan to follow"
	}

	return told + ", following the run plan in " + setup.Plan.Path
}

func ready(snapshot entity.Snapshot) string {
	branches := make([]string, 0, len(snapshot.Repositories))

	for _, repository := range snapshot.Repositories {
		branches = append(branches, repository.Branch)
	}

	if len(branches) == 0 {
		return "the workspace for this run is ready"
	}

	return fmt.Sprintf(
		"the workspace for this run is ready in %s, on %s",
		snapshot.Workspace, list(branches),
	)
}

func list(values []string) string {
	if len(values) == 1 {
		return values[0]
	}

	return fmt.Sprintf(
		"%s and %s", strings.Join(values[:len(values)-1], ", "), values[len(values)-1],
	)
}

func (s *executionsService) reused(
	ctx context.Context,
	execution entity.Execution,
) map[string]string {
	if execution.Attempt <= 1 || execution.IssueKey == "" {
		return nil
	}

	found, err := s.runs.LoadTasks(ctx)
	if err != nil {
		return nil
	}

	var previous entity.Execution

	for _, held := range found {
		if held.ID == execution.ID || held.IssueKey != execution.IssueKey {
			continue
		}

		if held.Attempt < execution.Attempt && held.Attempt > previous.Attempt {
			previous = held
		}
	}

	if previous.ID == "" {
		return nil
	}

	snapshot, err := s.runs.Load(ctx, previous.ID)
	if err != nil {
		if !errors.Is(err, entity.ErrSnapshotMissing) {
			logging.From(ctx).WarnContext(
				ctx,
				"this machine could not read what an earlier attempt at this issue left behind",
				slog.String("execution_id", previous.ID),
				slog.String("error", err.Error()),
			)
		}

		return nil
	}

	branches := make(map[string]string, len(snapshot.Repositories))

	for _, repository := range snapshot.Repositories {
		branches[repository.Name] = repository.Branch
	}

	return branches
}

func (s *executionsService) stop(executionID string) bool {
	cancel, underway := s.work[executionID]
	if !underway {
		return false
	}

	s.owed[executionID] = true

	cancel()

	return true
}

func (s *executionsService) List(ctx context.Context) ([]entity.Execution, error) {
	found, err := s.runs.LoadTasks(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for index, execution := range found {
		if live, holding := s.held[execution.ID]; holding {
			found[index] = live
		}
	}

	return found, nil
}

func (s *executionsService) Timeline(
	ctx context.Context,
	executionID string,
) ([]entity.TimelineEntry, error) {
	return s.runs.Timeline(ctx, executionID)
}
