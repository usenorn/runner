package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	api "github.com/usenorn/norn/pkg/http/v1/dashboard"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/dashboardclient"
	"github.com/usenorn/runner/internal/repository"
)

type httpUpload struct {
	client *dashboardclient.Client
	server string
}

func New(client *dashboardclient.Client, runner config.Runner) repository.Upload {
	return &httpUpload{client: client, server: runner.Server}
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

func (r *httpUpload) PublishArtifact(
	ctx context.Context,
	token string,
	executionID string,
	label string,
	body io.Reader,
) (entity.ArtifactReceipt, error) {
	var packed bytes.Buffer

	form := multipart.NewWriter(&packed)

	part, err := form.CreateFormFile("file", label)
	if err != nil {
		return entity.ArtifactReceipt{}, fmt.Errorf("wrap %s for norn: %w", label, err)
	}

	if _, err := io.Copy(part, body); err != nil {
		return entity.ArtifactReceipt{}, fmt.Errorf("read %s: %w", label, err)
	}

	if err := form.Close(); err != nil {
		return entity.ArtifactReceipt{}, fmt.Errorf("wrap %s for norn: %w", label, err)
	}

	response, err := r.client.UploadExecutionArtifactWithBodyWithResponse(
		ctx, executionID, form.FormDataContentType(), &packed, bearer(token),
	)
	if err != nil {
		return entity.ArtifactReceipt{}, r.unreachable(err)
	}

	if response.JSON201 == nil {
		return entity.ArtifactReceipt{}, r.refusal(response.HTTPResponse, response.Body)
	}

	return entity.ArtifactReceipt{
		ID:    response.JSON201.Id.String(),
		Label: response.JSON201.Name,
		Bytes: response.JSON201.Bytes,
	}, nil
}
