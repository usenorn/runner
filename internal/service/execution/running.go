package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/repository"
)

const timelineToolMax = 500

const finishedNote = "the coding agent has finished. Turning what it did into commits on a " +
	"branch, pull requests and a change set is not built into this release, so the run waits " +
	"here until it is cancelled. Its work is on the branches this machine made, in the " +
	"workspace for this run"

func (s *executionsService) Driver(ctx context.Context) entity.DriverHealth {
	return s.drivers.Preflight(ctx, entity.DriverClaude)
}

func (s *executionsService) drive(
	ctx context.Context,
	execution entity.Execution,
	snapshot entity.Snapshot,
	setup entity.RunSetup,
) error {
	if err := s.move(ctx, execution, channelv1.StateRunning, opening(setup)); err != nil {
		return err
	}

	execution.State = channelv1.StateRunning

	if _, err := s.uploads.Open(ctx, execution.ID); err != nil {
		if err := s.note(ctx, execution.ID, channelv1.EventNote, quiet(err)); err != nil {
			return err
		}
	}

	defer s.uploads.Close(context.WithoutCancel(ctx), execution.ID)

	result, err := s.sessions(ctx, execution, snapshot, setup)
	if err != nil {
		return err
	}

	if !result.Outcome.Finished() {
		return fmt.Errorf("%w: %s", entity.ErrDriverCrashed, ending(result))
	}

	if err := s.move(ctx, execution, channelv1.StateFinalizing, ending(result)); err != nil {
		return err
	}

	execution.State = channelv1.StateFinalizing

	return s.note(ctx, execution.ID, channelv1.EventPhase, finishedNote)
}

func (s *executionsService) sessions(
	ctx context.Context,
	execution entity.Execution,
	snapshot entity.Snapshot,
	setup entity.RunSetup,
) (entity.DriverResult, error) {
	env := s.env(execution, snapshot, setup)
	task := entity.ComposeTask(execution, snapshot, setup.Plan)

	session, err := s.drivers.Start(ctx, env, task)
	if err != nil {
		return entity.DriverResult{}, failure{step: entity.StepDriver, err: err}
	}

	result := s.attend(ctx, execution, session)

	for attempt := 0; result.Outcome == entity.OutcomeCrashed &&
		attempt < s.driver.ResumeAttempts; attempt++ {
		held := session.Reference()

		if held.ID == "" {
			break
		}

		if err := s.note(ctx, execution.ID, channelv1.EventNote, crashed(result)); err != nil {
			return result, err
		}

		s.resumed(ctx, execution.ID)

		session, err = s.drivers.Resume(ctx, env, held, entity.DriverResumeInjection)
		if err != nil {
			return result, failure{step: entity.StepDriver, err: err}
		}

		result = s.attend(ctx, execution, session)
	}

	return result, nil
}

func (s *executionsService) attend(
	ctx context.Context,
	execution entity.Execution,
	session repository.Session,
) entity.DriverResult {
	stopping, stop := context.WithCancel(context.WithoutCancel(ctx))
	defer stop()

	go func() {
		select {
		case <-ctx.Done():
			s.complain(stopping, execution.ID, session.Stop(stopping, s.driver.StopGrace))
		case <-stopping.Done():
		}
	}()

	events, logs, told := session.Events(), session.Logs(), 0

	for events != nil || logs != nil {
		select {
		case event, open := <-events:
			if !open {
				events = nil

				continue
			}

			s.uploads.Event(stopping, execution.ID, event)

			told = s.remark(stopping, execution.ID, event, told)
		case line, open := <-logs:
			if !open {
				logs = nil

				continue
			}

			s.uploads.Line(stopping, execution.ID, entity.LogLine{
				Stream: "stderr",
				Source: string(session.Reference().Outcome),
				Text:   line,
			})
		}
	}

	result, err := session.Wait()
	if err != nil {
		logging.From(stopping).WarnContext(
			stopping,
			"this machine could not tell how the coding agent finished",
			slog.String("execution_id", execution.ID),
			slog.String("error", err.Error()),
		)
	}

	s.record(stopping, execution.ID, entity.TimelineEntry{
		Kind:     channelv1.EventPhase,
		Reason:   ending(result),
		Occurred: s.now(),
	})

	s.settled(stopping, execution.ID, session.Reference())

	return result
}

// remark puts the coding agent's tool calls on the timeline, which is what a person reads, and
// stops once a run has put enough there: the full account is the transcript, and a run that calls
// a thousand tools would otherwise spend the channel on itself.
func (s *executionsService) remark(
	ctx context.Context,
	executionID string,
	event entity.DriverEvent,
	told int,
) int {
	if event.Kind != entity.DriverEventToolCall {
		return told
	}

	if told > timelineToolMax {
		return told
	}

	reason := "the coding agent used " + event.Tool

	if told == timelineToolMax {
		reason = fmt.Sprintf(
			"the coding agent has used %d tools, and the rest of them are in its transcript "+
				"rather than here", told,
		)
	}

	s.complain(ctx, executionID, s.note(ctx, executionID, channelv1.EventTool, reason))

	return told + 1
}

func (s *executionsService) settled(
	ctx context.Context,
	executionID string,
	held entity.DriverSession,
) {
	driver, err := s.runs.LoadDriver(ctx, executionID)
	if err != nil {
		s.complain(ctx, executionID, err)

		return
	}

	driver.Sessions = append(driver.Sessions, held)

	s.complain(ctx, executionID, s.runs.SaveDriver(ctx, executionID, driver))
}

func (s *executionsService) resumed(ctx context.Context, executionID string) {
	driver, err := s.runs.LoadDriver(ctx, executionID)
	if err != nil {
		s.complain(ctx, executionID, err)

		return
	}

	driver.Resumes++

	s.complain(ctx, executionID, s.runs.SaveDriver(ctx, executionID, driver))
}

func (s *executionsService) env(
	execution entity.Execution,
	snapshot entity.Snapshot,
	setup entity.RunSetup,
) entity.ExecEnv {
	values := slices.Clone(os.Environ())
	values = append(values, entity.ExecutionVariable+"="+execution.ID)

	return entity.ExecEnv{
		ExecutionID: execution.ID,
		Workspace:   workspaceOf(execution, snapshot),
		Environment: values,
		Profile:     setup.Permissions.Profile,
	}
}

func workspaceOf(execution entity.Execution, snapshot entity.Snapshot) string {
	if snapshot.Workspace != "" {
		return snapshot.Workspace
	}

	return filepath.Join(execution.Directory, entity.RunWorkspaceDir)
}

func opening(setup entity.RunSetup) string {
	return fmt.Sprintf(
		"%s is working on this run under the %s profile",
		setup.Driver.Kind, setup.Permissions.Profile,
	)
}

func ending(result entity.DriverResult) string {
	switch result.Outcome {
	case entity.OutcomeDone:
		return summed(result)
	case entity.OutcomeCrashed:
		return fmt.Sprintf(
			"the coding agent stopped without saying it was finished, and left exit code %d",
			result.ExitCode,
		)
	case entity.OutcomeNeedsInput:
		return entity.ErrDriverUnanswerable.Error() + ": " + strings.TrimSpace(result.Summary)
	default:
		return "the coding agent gave up: " + strings.TrimSpace(result.Summary)
	}
}

func summed(result entity.DriverResult) string {
	said := fmt.Sprintf(
		"the coding agent finished after %d turns in %s",
		result.Usage.Turns, result.Usage.Took.Round(time.Second),
	)

	if result.Denials > 0 {
		said += fmt.Sprintf(", with %d thing(s) it was not allowed to do", result.Denials)
	}

	if summary := strings.TrimSpace(result.Summary); summary != "" {
		return said + ": " + summary
	}

	return said
}

func crashed(result entity.DriverResult) string {
	return fmt.Sprintf(
		"%s. This machine is asking it to carry on from where it left off",
		strings.TrimSpace(ending(result)),
	)
}

func quiet(err error) string {
	if errors.Is(err, context.Canceled) {
		return "this run was stopped before its transcript could be sent to norn"
	}

	return "this machine cannot send norn what the coding agent is doing, and is holding it " +
		"back until it can: " + err.Error()
}
