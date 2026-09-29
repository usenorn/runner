package execution

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
)

type resumption struct {
	executionID string
	instruction channelv1.Instruction
}

func (s *executionsService) Continue(
	ctx context.Context,
	executionID string,
	instruction channelv1.Instruction,
) error {
	s.mu.Lock()
	execution, holding := s.held[executionID]
	s.mu.Unlock()

	if !holding {
		return nil
	}

	if instruction.Reason == channelv1.ResumeApproved && execution.State.Parked() {
		return s.approve(ctx, execution)
	}

	if !execution.State.Parked() {
		logging.From(ctx).InfoContext(
			ctx,
			"norn asked this machine to carry on with a run that is not parked",
			slog.String("execution_id", executionID),
			slog.String("state", string(execution.State)),
			slog.String("reason", instruction.Reason),
		)

		return nil
	}

	if err := s.runs.SaveResume(ctx, executionID, instruction); err != nil {
		return err
	}

	if _, err := s.queued(ctx, execution); err != nil {
		return err
	}

	s.admit(ctx, resuming(executionID, instruction))

	return nil
}

func resuming(executionID string, instruction channelv1.Instruction) admission {
	return admission{
		executionID: executionID,
		resume:      &resumption{executionID: executionID, instruction: instruction},
	}
}

func (s *executionsService) resume(base context.Context, held resumption) {
	ctx, stop := context.WithCancel(base)

	s.mu.Lock()
	execution, holding := s.held[held.executionID]

	if holding {
		s.work[held.executionID] = stop
	}
	s.mu.Unlock()

	if !holding {
		stop()

		return
	}

	err := s.carryOn(ctx, execution, held.instruction)

	s.mu.Lock()
	delete(s.work, held.executionID)
	owed := s.owed[held.executionID]
	delete(s.owed, held.executionID)
	s.mu.Unlock()

	stop()

	settled := context.WithoutCancel(base)

	switch {
	case owed:
		s.complain(settled, held.executionID, s.finished(settled, held.executionID))
	case base.Err() != nil:
	case err != nil:
		s.complain(settled, held.executionID, s.fail(settled, execution, err.Error()))
		s.complain(settled, held.executionID, s.finished(settled, held.executionID))
	}
}

type resumable struct {
	snapshot entity.Snapshot
	setup    entity.RunSetup
	session  entity.DriverSession
}

func (s *executionsService) resumable(
	ctx context.Context,
	executionID string,
) (resumable, error) {
	snapshot, err := s.runs.Load(ctx, executionID)
	if err != nil {
		return resumable{}, failure{step: entity.StepSnapshot, err: err}
	}

	setup, err := s.runs.LoadSetup(ctx, executionID)
	if err != nil {
		return resumable{}, failure{step: entity.StepSetup, err: err}
	}

	driver, err := s.runs.LoadDriver(ctx, executionID)
	if err != nil {
		return resumable{}, failure{step: entity.StepDriver, err: err}
	}

	last, held := driver.Latest()
	if !held || last.ID == "" {
		return resumable{}, failure{step: entity.StepDriver, err: entity.ErrDriverSessionUnknown}
	}

	return resumable{snapshot: snapshot, setup: setup, session: last}, nil
}

func (s *executionsService) carryOn(
	ctx context.Context,
	execution entity.Execution,
	instruction channelv1.Instruction,
) error {
	held, err := s.resumable(ctx, execution.ID)
	if err != nil {
		return err
	}

	s.restarting(execution.ID)

	if instruction.Stage.Valid() {
		execution.Stage = instruction.Stage
	}

	if err := s.move(ctx, execution, channelv1.StateRunning, resumed(instruction)); err != nil {
		return err
	}

	execution.State = channelv1.StateRunning

	s.complain(ctx, execution.ID, s.runs.ClearResume(ctx, execution.ID))

	if _, err := s.uploads.Open(ctx, execution.ID); err != nil {
		if err := s.note(ctx, execution.ID, channelv1.EventNote, quiet(err)); err != nil {
			return err
		}
	}

	defer s.uploads.Close(context.WithoutCancel(ctx), execution.ID)

	return s.again(ctx, execution, held, s.injection(ctx, execution.ID, instruction))
}

func (s *executionsService) again(
	ctx context.Context,
	execution entity.Execution,
	held resumable,
	injected string,
) error {
	if err := s.sandboxes.Open(ctx, entity.SandboxSpecFor(execution, held.snapshot)); err != nil {
		return failure{step: entity.StepSandbox, err: err}
	}

	env, err := s.tooling(ctx, execution, held.snapshot, held.setup)
	if err != nil {
		return failure{step: entity.StepDriver, err: err}
	}

	session, err := s.drivers.Resume(ctx, env, held.session, injected)
	if err != nil {
		return failure{step: entity.StepDriver, err: err}
	}

	s.resumed(ctx, execution.ID)

	return s.finish(ctx, execution, s.attend(ctx, execution, session))
}

// queued writes down the move norn already made. Asking a machine to carry on is norn saying it
// has queued the run for resume, and the state machine puts that state between waiting and running;
// reporting it back would be the machine claiming a state that is not its to claim.
func (s *executionsService) queued(
	ctx context.Context,
	execution entity.Execution,
) (entity.Execution, error) {
	if !execution.State.CanTransitionTo(channelv1.StateQueuedForResume) {
		return execution, fmt.Errorf(
			"%w: %s to %s",
			entity.ErrExecutionRefused, execution.State, channelv1.StateQueuedForResume,
		)
	}

	execution.State = channelv1.StateQueuedForResume

	if err := s.runs.SaveTask(ctx, execution); err != nil {
		return execution, err
	}

	s.mu.Lock()
	s.held[execution.ID] = execution
	s.mu.Unlock()

	return execution, nil
}

// injection settles every question this run stopped on and grounds the agent in what it asked,
// not only in what came back. Being asked to carry on is the answers arriving, whether or not they
// also came down the channel on their own, so a run told to resume never stays holding a question
// and parking again on one nobody is still waiting to answer.
func (s *executionsService) injection(
	ctx context.Context,
	executionID string,
	instruction channelv1.Instruction,
) string {
	switch instruction.Reason {
	case channelv1.ResumePlanApproved:
		return entity.PlanApprovedInjection(instruction.Instruction)
	case channelv1.ResumePlanRevision:
		return entity.PlanRevisionInjection(instruction.Instruction)
	case channelv1.ResumeAnswer:
		return s.answered(ctx, executionID, instruction)
	default:
		return strings.TrimSpace(instruction.Instruction)
	}
}

func (s *executionsService) answered(
	ctx context.Context,
	executionID string,
	instruction channelv1.Instruction,
) string {
	answers := make([]entity.Answer, 0, len(instruction.Answers))

	for _, given := range instruction.Answers {
		answer := entity.AnswerOf(given)
		answers = append(answers, answer)

		s.complain(ctx, executionID, s.questions.Answered(ctx, executionID, answer))
	}

	s.complain(ctx, executionID, s.questions.Forget(ctx, executionID))

	if len(answers) == 0 {
		return strings.TrimSpace(instruction.Instruction)
	}

	return entity.AnswersInjection(answers)
}

func resumed(instruction channelv1.Instruction) string {
	switch instruction.Reason {
	case channelv1.ResumePlanApproved:
		return "somebody approved the plan, and the coding agent is building it"
	case channelv1.ResumePlanRevision:
		return "somebody asked for the plan to change, and the coding agent is revising it"
	case channelv1.ResumeAnswer:
		return "somebody answered, and the coding agent is carrying on from where it stopped"
	}

	said := strings.TrimSpace(instruction.Instruction)
	if said == "" {
		return "norn asked this machine to carry on"
	}

	return "norn asked this machine to carry on: " + said
}
