package upload_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
	uploadrepo "github.com/usenorn/runner/internal/repository/upload"
	"github.com/usenorn/runner/internal/service"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
	uploadsvc "github.com/usenorn/runner/internal/service/upload"
)

type harness struct {
	posts     *uploadrepo.MockUpload
	dashboard *dashboardrepo.MockDashboard
	sessions  *sessionsvc.MockSessions
	service   service.Uploads

	mode    entity.TelemetryMode
	cursors []entity.StreamCursor

	mu          sync.Mutex
	refuse      error
	refusals    int
	transcripts []entity.TranscriptBatch
	logs        []entity.LogBatch
}

func newHarness(t *testing.T, cfg config.Upload) *harness {
	t.Helper()

	controller := gomock.NewController(t)

	h := &harness{
		posts:     uploadrepo.NewMockUpload(controller),
		dashboard: dashboardrepo.NewMockDashboard(controller),
		sessions:  sessionsvc.NewMockSessions(controller),
		mode:      entity.TelemetryFull,
	}

	h.sessions.EXPECT().Access(gomock.Any()).Return("access-token", nil).AnyTimes()

	h.dashboard.EXPECT().
		Telemetry(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (entity.TelemetryMode, error) {
			return h.mode, nil
		}).
		AnyTimes()

	h.posts.EXPECT().
		Cursors(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, string) ([]entity.StreamCursor, error) {
			return h.cursors, nil
		}).
		AnyTimes()

	h.posts.EXPECT().
		AppendTranscript(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(h.appendTranscript).
		AnyTimes()

	h.posts.EXPECT().
		AppendLogs(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(h.appendLogs).
		AnyTimes()

	h.service = uploadsvc.New(h.posts, h.dashboard, h.sessions, cfg)

	return h
}

func settings() config.Upload {
	return config.Upload{
		Enabled:       true,
		Batch:         2,
		Flush:         10 * time.Millisecond,
		MaxChunkBytes: 1 << 20,
		MaxPending:    4,
	}
}

func (h *harness) appendTranscript(
	_ context.Context,
	_ string,
	_ string,
	batch entity.TranscriptBatch,
) (entity.UploadReceipt, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.refuse != nil {
		h.refusals++

		return entity.UploadReceipt{}, h.refuse
	}

	h.transcripts = append(h.transcripts, batch)

	return entity.UploadReceipt{Stream: entity.StreamTranscript, Sequence: batch.Sequence}, nil
}

func (h *harness) appendLogs(
	_ context.Context,
	_ string,
	_ string,
	batch entity.LogBatch,
) (entity.UploadReceipt, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.refuse != nil {
		h.refusals++

		return entity.UploadReceipt{}, h.refuse
	}

	h.logs = append(h.logs, batch)

	return entity.UploadReceipt{Stream: entity.StreamLogs, Sequence: batch.Sequence}, nil
}

func (h *harness) sent() []entity.TranscriptBatch {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]entity.TranscriptBatch(nil), h.transcripts...)
}

func (h *harness) said() []entity.LogBatch {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]entity.LogBatch(nil), h.logs...)
}

func (h *harness) refusing(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.refuse = err
}

func said(text string) entity.DriverEvent {
	return entity.DriverEvent{Kind: entity.DriverEventMessage, Text: text}
}

func used(tool string) entity.DriverEvent {
	return entity.DriverEvent{Kind: entity.DriverEventToolCall, Tool: tool}
}
