package upload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

const driverSource = "coding agent"

type stream struct {
	next    int64
	waiting []entity.DriverEvent
	lines   []entity.LogLine
	pending []pending
}

type pending struct {
	stream   entity.UploadStream
	sequence int64
	events   []entity.DriverEvent
	lines    []entity.LogLine
}

type run struct {
	mode       entity.TelemetryMode
	transcript stream
	logs       stream
	complained bool
	dropped    bool
}

type uploadsService struct {
	uploads   repository.Upload
	dashboard repository.Dashboard
	sessions  service.Sessions
	cfg       config.Upload
	now       func() time.Time

	mu   sync.Mutex
	held map[string]*run
}

func New(
	uploads repository.Upload,
	dashboard repository.Dashboard,
	sessions service.Sessions,
	cfg config.Upload,
) service.Uploads {
	return &uploadsService{
		uploads:   uploads,
		dashboard: dashboard,
		sessions:  sessions,
		cfg:       cfg,
		now:       func() time.Time { return time.Now().UTC() },
		held:      map[string]*run{},
	}
}

func (s *uploadsService) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Flush)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.drain(context.WithoutCancel(ctx))

			return
		case <-ticker.C:
			s.drain(ctx)
		}
	}
}

func (s *uploadsService) drain(ctx context.Context) {
	s.mu.Lock()

	held := make([]string, 0, len(s.held))

	for executionID := range s.held {
		held = append(held, executionID)
	}

	s.mu.Unlock()

	for _, executionID := range held {
		if err := s.Flush(ctx, executionID); err != nil {
			s.complain(ctx, executionID, err)
		}
	}
}

func (s *uploadsService) Open(
	ctx context.Context,
	executionID string,
) (entity.TelemetryMode, error) {
	if !s.cfg.Enabled {
		s.remember(executionID, entity.TelemetryFull, nil)

		return entity.TelemetryFull, nil
	}

	token, err := s.sessions.Access(ctx)
	if err != nil {
		return "", err
	}

	mode, err := s.dashboard.Telemetry(ctx, token)
	if err != nil {
		return "", err
	}

	cursors, err := s.uploads.Cursors(ctx, token, executionID)
	if err != nil {
		return "", err
	}

	s.remember(executionID, mode, cursors)

	return mode, nil
}

func (s *uploadsService) remember(
	executionID string,
	mode entity.TelemetryMode,
	cursors []entity.StreamCursor,
) {
	held := &run{
		mode:       mode,
		transcript: stream{next: 1},
		logs:       stream{next: 1},
	}

	for _, cursor := range cursors {
		switch cursor.Stream {
		case entity.StreamTranscript:
			held.transcript.next = cursor.LastSequence + 1
		case entity.StreamLogs:
			held.logs.next = cursor.LastSequence + 1
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.held[executionID] = held
}

func (s *uploadsService) Event(
	ctx context.Context,
	executionID string,
	event entity.DriverEvent,
) {
	s.mu.Lock()

	held, holding := s.held[executionID]
	if !holding {
		s.mu.Unlock()

		return
	}

	if !held.mode.Keeps(entity.StreamTranscript) {
		line := summarise(event)
		s.mu.Unlock()

		if line.Text != "" {
			s.Line(ctx, executionID, line)
		}

		return
	}

	if event.At.IsZero() {
		event.At = s.now()
	}

	held.transcript.waiting = append(held.transcript.waiting, event)
	full := len(held.transcript.waiting) >= s.cfg.Batch

	s.mu.Unlock()

	if full {
		s.complain(ctx, executionID, s.Flush(ctx, executionID))
	}
}

func (s *uploadsService) Line(ctx context.Context, executionID string, line entity.LogLine) {
	s.mu.Lock()

	held, holding := s.held[executionID]
	if !holding {
		s.mu.Unlock()

		return
	}

	if line.At.IsZero() {
		line.At = s.now()
	}

	held.logs.lines = append(held.logs.lines, line)
	full := len(held.logs.lines) >= s.cfg.Batch

	s.mu.Unlock()

	if full {
		s.complain(ctx, executionID, s.Flush(ctx, executionID))
	}
}

func (s *uploadsService) Flush(ctx context.Context, executionID string) error {
	if !s.cfg.Enabled {
		return nil
	}

	s.mu.Lock()

	held, holding := s.held[executionID]
	if !holding {
		s.mu.Unlock()

		return nil
	}

	s.queue(held)

	queued := append(take(&held.transcript), take(&held.logs)...)

	s.mu.Unlock()

	if len(queued) == 0 {
		return nil
	}

	token, err := s.sessions.Access(ctx)
	if err != nil {
		s.giveBack(ctx, executionID, queued)

		return err
	}

	for at, batch := range queued {
		if err := s.deliver(ctx, token, executionID, batch); err != nil {
			s.giveBack(ctx, executionID, queued[at:])

			return err
		}
	}

	s.settled(executionID)

	return nil
}

func (s *uploadsService) settled(executionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if held, holding := s.held[executionID]; holding {
		held.complained = false
	}
}

func (s *uploadsService) deliver(
	ctx context.Context,
	token string,
	executionID string,
	batch pending,
) error {
	var err error

	switch batch.stream {
	case entity.StreamTranscript:
		_, err = s.uploads.AppendTranscript(ctx, token, executionID, entity.TranscriptBatch{
			Sequence: batch.sequence,
			Entries:  batch.events,
		})
	case entity.StreamLogs:
		_, err = s.uploads.AppendLogs(ctx, token, executionID, entity.LogBatch{
			Sequence: batch.sequence,
			Entries:  batch.lines,
		})
	}

	// A position norn already holds and a batch it will never take are both settled: keeping either
	// would stall every batch behind it, and the stream would stop at the first one norn refused.
	if errors.Is(err, entity.ErrUploadPositionTaken) ||
		errors.Is(err, entity.ErrUploadTooLarge) ||
		errors.Is(err, entity.ErrUploadRefused) {
		logging.From(ctx).WarnContext(
			ctx,
			"norn would not take a batch of this run's output, and it was dropped",
			slog.String("execution_id", executionID),
			slog.String("stream", string(batch.stream)),
			slog.Int64("sequence", batch.sequence),
			slog.String("error", err.Error()),
		)

		return nil
	}

	return err
}

func (s *uploadsService) queue(held *run) {
	if waiting := len(held.transcript.waiting); waiting > 0 {
		for _, part := range s.split(held.transcript.waiting) {
			held.transcript.pending = append(held.transcript.pending, pending{
				stream:   entity.StreamTranscript,
				sequence: held.transcript.next,
				events:   part,
			})

			held.transcript.next++
		}

		held.transcript.waiting = nil
	}

	if waiting := len(held.logs.lines); waiting > 0 {
		for _, part := range s.splitLines(held.logs.lines) {
			held.logs.pending = append(held.logs.pending, pending{
				stream:   entity.StreamLogs,
				sequence: held.logs.next,
				lines:    part,
			})

			held.logs.next++
		}

		held.logs.lines = nil
	}
}

func take(held *stream) []pending {
	queued := held.pending
	held.pending = nil

	return queued
}

func (s *uploadsService) giveBack(ctx context.Context, executionID string, queued []pending) {
	s.mu.Lock()

	held, holding := s.held[executionID]
	if !holding {
		s.mu.Unlock()

		return
	}

	for _, batch := range queued {
		switch batch.stream {
		case entity.StreamTranscript:
			held.transcript.pending = append(held.transcript.pending, batch)
		case entity.StreamLogs:
			held.logs.pending = append(held.logs.pending, batch)
		}
	}

	dropped := s.trim(&held.transcript) + s.trim(&held.logs)
	told := held.dropped

	if dropped > 0 {
		held.dropped = true
	}

	s.mu.Unlock()

	if dropped == 0 || told {
		return
	}

	logging.From(ctx).WarnContext(
		ctx,
		"norn has been out of reach long enough that the oldest of this run's output was dropped",
		slog.String("execution_id", executionID),
		slog.Int("batches", dropped),
	)
}

// trim keeps the newest batches when norn has been out of reach long enough for them to pile up.
// Dropping the oldest rather than refusing the newest keeps what a person is most likely to want.
func (s *uploadsService) trim(held *stream) int {
	over := len(held.pending) - s.cfg.MaxPending
	if over <= 0 {
		return 0
	}

	held.pending = held.pending[over:]

	return over
}

func (s *uploadsService) Close(ctx context.Context, executionID string) {
	s.complain(ctx, executionID, s.Flush(ctx, executionID))

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.held, executionID)
}

func (s *uploadsService) complain(ctx context.Context, executionID string, err error) {
	if err == nil {
		return
	}

	s.mu.Lock()

	held, holding := s.held[executionID]
	told := holding && held.complained

	if holding {
		held.complained = true
	}

	s.mu.Unlock()

	if told {
		return
	}

	logging.From(ctx).WarnContext(
		ctx,
		"this machine could not send norn what the coding agent is doing, and is holding it back",
		slog.String("execution_id", executionID),
		slog.String("error", err.Error()),
	)
}

func (s *uploadsService) split(events []entity.DriverEvent) [][]entity.DriverEvent {
	parts := make([][]entity.DriverEvent, 0, 1)

	held, size := make([]entity.DriverEvent, 0, len(events)), 0

	for _, event := range events {
		weight := weigh(event)

		if len(held) > 0 && size+weight > int(s.cfg.MaxChunkBytes) {
			parts = append(parts, held)
			held, size = make([]entity.DriverEvent, 0, len(events)), 0
		}

		held = append(held, event)
		size += weight
	}

	if len(held) > 0 {
		parts = append(parts, held)
	}

	return parts
}

func (s *uploadsService) splitLines(lines []entity.LogLine) [][]entity.LogLine {
	parts := make([][]entity.LogLine, 0, 1)

	held, size := make([]entity.LogLine, 0, len(lines)), 0

	for _, line := range lines {
		weight := len(line.Text) + len(line.Source) + len(line.Stream)

		if len(held) > 0 && size+weight > int(s.cfg.MaxChunkBytes) {
			parts = append(parts, held)
			held, size = make([]entity.LogLine, 0, len(lines)), 0
		}

		held = append(held, line)
		size += weight
	}

	if len(held) > 0 {
		parts = append(parts, held)
	}

	return parts
}

func weigh(event entity.DriverEvent) int {
	size := len(event.Text) + len(event.Tool) + len(event.Kind)

	if len(event.Payload) == 0 {
		return size
	}

	raw, err := json.Marshal(event.Payload)
	if err != nil {
		return size
	}

	return size + len(raw)
}

func summarise(event entity.DriverEvent) entity.LogLine {
	line := entity.LogLine{At: event.At, Stream: string(event.Kind), Source: driverSource}

	switch event.Kind {
	case entity.DriverEventToolCall:
		line.Text = "used " + event.Tool
	case entity.DriverEventToolResult:
		line.Text = fmt.Sprintf("%s answered in %d characters", event.Tool, len(event.Text))
	case entity.DriverEventMessage:
		line.Text = fmt.Sprintf("said %d characters", len(event.Text))
	case entity.DriverEventUsage:
		line.Text = "reported what the turn cost"
	default:
		line.Text = strings.TrimSpace(event.Text)
	}

	return line
}
