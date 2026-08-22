package upload

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	api "github.com/usenorn/norn/pkg/http/v1/dashboard"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/dashboardclient"
	"github.com/usenorn/runner/internal/repository"
)

const jsonContentType = "application/json"

type httpUpload struct {
	client *dashboardclient.Client
	server string
}

func New(client *dashboardclient.Client, runner config.Runner) repository.Upload {
	return &httpUpload{client: client, server: runner.Server}
}

func (r *httpUpload) AppendLogs(
	ctx context.Context,
	token string,
	executionID string,
	batch entity.LogBatch,
) (entity.UploadReceipt, error) {
	entries := make([]api.ExecutionLogEntry, 0, len(batch.Entries))

	for _, line := range batch.Entries {
		entries = append(entries, api.ExecutionLogEntry{
			At:     stamp(line.At),
			Stream: named(line.Stream),
			Source: named(line.Source),
			Text:   line.Text,
		})
	}

	packed, err := pack(api.UploadExecutionLogsRequest{
		Sequence: batch.Sequence,
		Entries:  entries,
	})
	if err != nil {
		return entity.UploadReceipt{}, err
	}

	response, err := r.client.UploadExecutionLogsWithBodyWithResponse(
		ctx, executionID, jsonContentType, bytes.NewReader(packed), bearer(token), compressed(),
	)
	if err != nil {
		return entity.UploadReceipt{}, r.unreachable(err)
	}

	if response.JSON202 == nil {
		return entity.UploadReceipt{}, r.refusal(response.HTTPResponse, response.Body)
	}

	return receiptOf(*response.JSON202), nil
}

func (r *httpUpload) AppendTranscript(
	ctx context.Context,
	token string,
	executionID string,
	batch entity.TranscriptBatch,
) (entity.UploadReceipt, error) {
	entries := make([]api.ExecutionTranscriptEntry, 0, len(batch.Entries))

	for _, event := range batch.Entries {
		entries = append(entries, api.ExecutionTranscriptEntry{
			At:      stamp(event.At),
			Type:    string(event.Kind),
			Payload: carried(event),
		})
	}

	packed, err := pack(api.UploadExecutionTranscriptRequest{
		Sequence: batch.Sequence,
		Entries:  entries,
	})
	if err != nil {
		return entity.UploadReceipt{}, err
	}

	response, err := r.client.UploadExecutionTranscriptWithBodyWithResponse(
		ctx, executionID, jsonContentType, bytes.NewReader(packed), bearer(token), compressed(),
	)
	if err != nil {
		return entity.UploadReceipt{}, r.unreachable(err)
	}

	if response.JSON202 == nil {
		return entity.UploadReceipt{}, r.refusal(response.HTTPResponse, response.Body)
	}

	return receiptOf(*response.JSON202), nil
}

func (r *httpUpload) Cursors(
	ctx context.Context,
	token string,
	executionID string,
) ([]entity.StreamCursor, error) {
	response, err := r.client.GetExecutionStreamsWithResponse(ctx, executionID, bearer(token))
	if err != nil {
		return nil, r.unreachable(err)
	}

	if response.JSON200 == nil {
		return nil, r.refusal(response.HTTPResponse, response.Body)
	}

	cursors := make([]entity.StreamCursor, 0, len(*response.JSON200))

	for _, held := range *response.JSON200 {
		cursors = append(cursors, entity.StreamCursor{
			Stream:       entity.UploadStream(held.Stream),
			LastSequence: held.LastSequence,
			Chunks:       held.Chunks,
			Entries:      held.EntryCount,
			Bytes:        held.Bytes,
		})
	}

	return cursors, nil
}

func receiptOf(held api.ExecutionChunkReceipt) entity.UploadReceipt {
	return entity.UploadReceipt{
		Stream:    entity.UploadStream(held.Stream),
		Sequence:  held.Sequence,
		Digest:    held.Digest,
		Duplicate: held.Duplicate,
	}
}

func carried(event entity.DriverEvent) *map[string]any {
	payload := map[string]any{}

	for key, value := range event.Payload {
		payload[key] = value
	}

	if event.Text != "" {
		payload["text"] = event.Text
	}

	if event.Tool != "" {
		payload["tool"] = event.Tool
	}

	if len(payload) == 0 {
		return nil
	}

	return &payload
}

func pack(body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("write a batch for norn: %w", err)
	}

	var held bytes.Buffer

	writer := gzip.NewWriter(&held)

	if _, err := writer.Write(raw); err != nil {
		return nil, fmt.Errorf("compress a batch for norn: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("compress a batch for norn: %w", err)
	}

	return held.Bytes(), nil
}

func compressed() api.RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("Content-Encoding", "gzip")

		return nil
	}
}

func bearer(token string) api.RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

		return nil
	}
}

func (r *httpUpload) refusal(response *http.Response, body []byte) error {
	switch response.StatusCode {
	case http.StatusConflict:
		return entity.ErrUploadPositionTaken
	case http.StatusRequestEntityTooLarge:
		return entity.ErrUploadTooLarge
	case http.StatusUnprocessableEntity:
		return refusedBy(body, entity.ErrUploadRefused)
	case http.StatusNotFound:
		return entity.ErrUploadUnknownRun
	}

	if detail := detailOf(body); detail != "" {
		return fmt.Errorf("norn answered %s: %s", response.Status, detail)
	}

	return fmt.Errorf("norn answered %s", response.Status)
}

func (r *httpUpload) unreachable(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return fmt.Errorf("%w at %s: %w", entity.ErrServerUnreachable, r.server, err)
}

func refusedBy(body []byte, fallback error) error {
	if detail := detailOf(body); detail != "" {
		return fmt.Errorf("%w: %s", fallback, detail)
	}

	return fallback
}

func detailOf(body []byte) string {
	var problem struct {
		Detail string `json:"detail"`
	}

	if err := json.Unmarshal(body, &problem); err != nil {
		return ""
	}

	return strings.TrimSpace(problem.Detail)
}

func stamp(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}

	held := at.UTC()

	return &held
}

func named(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return &value
}
