package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

type previewsService struct {
	runs    repository.Run
	spool   repository.Spool
	serving service.Sessions

	now func() time.Time

	mu   sync.Mutex
	held map[string]map[string]entity.Preview
}

func New(runs repository.Run, spool repository.Spool, serving service.Sessions) service.Previews {
	return &previewsService{
		runs:    runs,
		spool:   spool,
		serving: serving,
		now:     func() time.Time { return time.Now().UTC() },
		held:    map[string]map[string]entity.Preview{},
	}
}

func (s *previewsService) Expose(
	ctx context.Context,
	executionID string,
	wanted entity.Preview,
) (entity.Preview, error) {
	if wanted.Name == "" {
		wanted.Name = wanted.Service
	}

	if err := wanted.Valid(); err != nil {
		return entity.Preview{}, err
	}

	execution, err := s.claim(ctx, executionID)
	if err != nil {
		return entity.Preview{}, err
	}

	record, err := s.running(ctx, execution, wanted.Service)
	if err != nil {
		return entity.Preview{}, err
	}

	exposed := entity.Preview{
		Name:      wanted.Name,
		Service:   wanted.Service,
		Path:      wanted.Path,
		Port:      record.Port,
		URL:       entity.PreviewURL(record.Port, wanted.Path),
		ExposedAt: s.now(),
	}

	left, err := s.hold(executionID, exposed)
	if err != nil {
		return entity.Preview{}, err
	}

	if left.Port != 0 {
		s.tell(ctx, executionID, left, channelv1.PreviewClosed, left.Name+" is closed")
	}

	s.tell(ctx, executionID, exposed, channelv1.PreviewOpen, fmt.Sprintf(
		"%s is open at %s, on the service %s", exposed.Name, exposed.URL, exposed.Service,
	))

	return s.addressed(execution, exposed), nil
}

func (s *previewsService) hold(
	executionID string,
	exposed entity.Preview,
) (entity.Preview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	holding, known := s.held[executionID]
	if !known {
		holding = map[string]entity.Preview{}
		s.held[executionID] = holding
	}

	for name, standing := range holding {
		if name != exposed.Name && standing.Port == exposed.Port {
			return entity.Preview{}, fmt.Errorf(
				"%w: %s already opens the port %s is on, and one port carries one address",
				entity.ErrPreviewInvalid, name, exposed.Service,
			)
		}
	}

	standing, taken := holding[exposed.Name]
	if !taken && len(holding) >= entity.PreviewsMax {
		return entity.Preview{}, fmt.Errorf("%w: %d", entity.ErrPreviewCrowded, entity.PreviewsMax)
	}

	holding[exposed.Name] = exposed

	if taken && standing.Port != exposed.Port {
		return standing, nil
	}

	return entity.Preview{}, nil
}

func (s *previewsService) Close(
	ctx context.Context,
	executionID string,
	name string,
) (entity.Preview, error) {
	execution, err := s.claim(ctx, executionID)
	if err != nil {
		return entity.Preview{}, err
	}

	s.mu.Lock()

	closed, standing := s.held[executionID][name]
	if standing {
		delete(s.held[executionID], name)
	}

	s.mu.Unlock()

	if !standing {
		return entity.Preview{}, fmt.Errorf("%w: %s", entity.ErrPreviewUnknown, name)
	}

	s.tell(ctx, executionID, closed, channelv1.PreviewClosed, closed.Name+" is closed")

	return s.addressed(execution, closed), nil
}

func (s *previewsService) Resolve(
	ctx context.Context,
	executionID string,
	name string,
) (entity.Preview, error) {
	s.mu.Lock()
	held, standing := s.held[executionID][name]
	s.mu.Unlock()

	if !standing {
		return entity.Preview{}, fmt.Errorf("%w: %s", entity.ErrPreviewUnknown, name)
	}

	execution, err := s.claim(ctx, executionID)
	if err != nil {
		return entity.Preview{}, err
	}

	record, err := s.running(ctx, execution, held.Service)
	if err != nil {
		return entity.Preview{}, err
	}

	held.Port = record.Port
	held.URL = entity.PreviewURL(record.Port, held.Path)

	return held, nil
}

func (s *previewsService) List(
	ctx context.Context,
	executionID string,
) ([]entity.Preview, error) {
	execution, err := s.claim(ctx, executionID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	open := make([]entity.Preview, 0, len(s.held[executionID]))

	for _, preview := range s.held[executionID] {
		open = append(open, s.addressed(execution, preview))
	}

	sort.Slice(open, func(i, j int) bool { return open[i].Name < open[j].Name })

	return open, nil
}

func (s *previewsService) Release(ctx context.Context, executionID string) {
	s.mu.Lock()

	standing := make([]entity.Preview, 0, len(s.held[executionID]))

	for _, preview := range s.held[executionID] {
		standing = append(standing, preview)
	}

	delete(s.held, executionID)

	s.mu.Unlock()

	sort.Slice(standing, func(i, j int) bool { return standing[i].Name < standing[j].Name })

	for _, preview := range standing {
		s.tell(
			ctx, executionID, preview, channelv1.PreviewClosed, preview.Name+" is closed",
		)
	}
}

func (s *previewsService) addressed(
	execution entity.Execution,
	preview entity.Preview,
) entity.Preview {
	preview.Shared = s.serving.Previews().Address(execution, preview.Port, preview.Path)

	return preview
}

func (s *previewsService) running(
	ctx context.Context,
	execution entity.Execution,
	name string,
) (entity.ServiceRecord, error) {
	services, err := s.runs.LoadServices(ctx, execution.ID)
	if err != nil {
		return entity.ServiceRecord{}, err
	}

	record, running := services.Service(name)
	if !running {
		return entity.ServiceRecord{}, fmt.Errorf(
			"%w: this run is not running %s", entity.ErrPreviewNotOwned, name,
		)
	}

	if record.State != entity.ServiceHealthy {
		return entity.ServiceRecord{}, fmt.Errorf(
			"%w: %s is %s, and a preview opens on a service that is healthy",
			entity.ErrPreviewNotOwned, name, record.State,
		)
	}

	if record.Port == 0 {
		return entity.ServiceRecord{}, fmt.Errorf(
			"%w: %s holds no port to open", entity.ErrPreviewNotOwned, name,
		)
	}

	return record, nil
}

func (s *previewsService) claim(
	ctx context.Context,
	executionID string,
) (entity.Execution, error) {
	execution, err := s.runs.LoadTask(ctx, executionID)
	if err != nil {
		return entity.Execution{}, err
	}

	if execution.Finished() {
		return entity.Execution{}, fmt.Errorf(
			"%w: %s has already finished as %s",
			entity.ErrExecutionRefused, executionID, execution.State,
		)
	}

	return execution, nil
}

func (s *previewsService) tell(
	ctx context.Context,
	executionID string,
	preview entity.Preview,
	state string,
	reason string,
) {
	ctx = context.WithoutCancel(ctx)

	entry := entity.TimelineEntry{
		Kind:     channelv1.EventPreview,
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

	s.register(ctx, executionID, preview, state, entry.Occurred)
}

func (s *previewsService) register(
	ctx context.Context,
	executionID string,
	preview entity.Preview,
	state string,
	occurred time.Time,
) {
	raw, err := json.Marshal(channelv1.Preview{
		Name:     preview.Name,
		Service:  preview.Service,
		Path:     preview.Path,
		Port:     preview.Port,
		State:    state,
		Occurred: occurred,
	})
	if err != nil {
		return
	}

	message, err := channelv1.NewRunnerMessage(
		channelv1.PreviewState, executionID, raw, s.now(),
	)
	if err != nil {
		return
	}

	if err := s.spool.Append(ctx, message); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not register a preview with norn, so nothing off this machine "+
				"will reach it",
			slog.String("execution_id", executionID),
			slog.String("preview", preview.Name),
			slog.String("error", err.Error()),
		)
	}
}
