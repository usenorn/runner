package execution

import (
	"context"
	"errors"
	"fmt"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func (s *executionsService) prepareReview(
	ctx context.Context,
	execution entity.Execution,
) (entity.ReviewPass, error) {
	pass := entity.ReviewPass{Revision: execution.Revision + 1}

	setup, err := s.runs.LoadSetup(ctx, execution.ID)
	if err != nil {
		return pass, err
	}

	if setup.Plan.Source != entity.PlanCodebase || setup.Plan.Path == "" {
		return pass, nil
	}

	plan, err := s.settings.Definition(ctx, setup.Plan.Path)
	if err != nil {
		return pass, s.note(ctx, execution.ID, channelv1.EventPreview, entity.PlanUnreadable(err))
	}

	failures, err := s.bringUp(ctx, execution.ID, plan.Runnable())
	if err != nil {
		return pass, err
	}

	for _, preview := range plan.Previews {
		pass.Previews = append(pass.Previews, s.expose(ctx, execution.ID, plan, preview, failures))
	}

	return pass, nil
}

func (s *executionsService) bringUp(
	ctx context.Context,
	executionID string,
	services []entity.Service,
) (map[string]string, error) {
	for index := len(services) - 1; index >= 0; index-- {
		name := services[index].Name

		_, err := s.services.Stop(ctx, executionID, name)
		if err == nil || errors.Is(err, entity.ErrServiceUnknown) {
			continue
		}

		if err := s.note(
			ctx, executionID, channelv1.EventService, entity.ServiceUnstoppable(name, err),
		); err != nil {
			return nil, err
		}
	}

	failures := map[string]string{}

	for _, service := range services {
		if reason, blocked := blockedBy(service, failures); blocked {
			failures[service.Name] = reason

			continue
		}

		if _, err := s.services.Start(ctx, executionID, service); err != nil {
			failures[service.Name] = err.Error()

			continue
		}

		record, err := s.services.Await(ctx, executionID, service.Name)
		if err != nil {
			failures[service.Name] = err.Error()

			continue
		}

		if record.State != entity.ServiceHealthy {
			failures[service.Name] = unhealthyReason(record)
		}
	}

	return failures, nil
}

func blockedBy(service entity.Service, failures map[string]string) (string, bool) {
	for _, needed := range service.Requires {
		if _, failed := failures[needed]; failed {
			return fmt.Sprintf("it needs %s, which did not come up", needed), true
		}
	}

	return "", false
}

func unhealthyReason(record entity.ServiceRecord) string {
	if record.State == entity.ServiceStarting {
		return "it was still starting when this machine stopped waiting for it"
	}

	if record.Reason == "" {
		return fmt.Sprintf("it is %s", record.State)
	}

	return record.Reason
}

func (s *executionsService) expose(
	ctx context.Context,
	executionID string,
	plan entity.PlanDefinition,
	preview entity.PlanPreview,
	failures map[string]string,
) entity.PreviewOutcome {
	outcome := entity.PreviewOutcome{Name: preview.Name, Service: preview.Service, Path: preview.Path}

	if reason, unrunnable := plan.Unrunnable(preview.Service); unrunnable {
		outcome.State = entity.PreviewOutcomeUnsupported
		outcome.Reason = reason

		return outcome
	}

	if reason, failed := failures[preview.Service]; failed {
		outcome.State = entity.PreviewOutcomeFailed
		outcome.Reason = reason

		return outcome
	}

	exposed, err := s.previews.Expose(ctx, executionID, entity.Preview{
		Name:    preview.Slug(),
		Service: preview.Service,
		Path:    preview.Path,
	})
	if errors.Is(err, entity.ErrPreviewInvalid) {
		outcome.State = entity.PreviewOutcomeUnsupported
		outcome.Reason = err.Error()

		return outcome
	}

	if err != nil {
		outcome.State = entity.PreviewOutcomeFailed
		outcome.Reason = err.Error()

		return outcome
	}

	outcome.State = entity.PreviewOutcomeReady
	outcome.Port = exposed.Port

	return outcome
}
