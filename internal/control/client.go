package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type Client struct {
	http      *http.Client
	cfg       config.Control
	questions config.Questions
	path      string
	token     string
}

type Bearer string

func NewBearer() Bearer {
	return Bearer(os.Getenv(entity.ExecutionTokenVariable))
}

func NewClient(
	cfg config.Control,
	questions config.Questions,
	dir *statedir.Dir,
	bearer Bearer,
) *Client {
	path := dir.Socket()

	return &Client{
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					dialer := &net.Dialer{Timeout: cfg.DialTimeout}

					return dialer.DialContext(ctx, "unix", path)
				},
			},
		},
		cfg:       cfg,
		questions: questions,
		path:      path,
		token:     string(bearer),
	}
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	return ask[Status](ctx, c, http.MethodGet, StatusPath, nil)
}

func (c *Client) Version(ctx context.Context) (Build, error) {
	return ask[Build](ctx, c, http.MethodGet, VersionPath, nil)
}

func (c *Client) Connect(ctx context.Context, request ConnectRequest) (Connected, error) {
	return ask[Connected](ctx, c, http.MethodPost, ConnectPath, request)
}

func (c *Client) Inspect(ctx context.Context, root string) (Scan, error) {
	return ask[Scan](ctx, c, http.MethodPost, InspectPath, InspectRequest{Root: root})
}

func (c *Client) Accept(ctx context.Context, scan Scan) (Accepted, error) {
	return ask[Accepted](ctx, c, http.MethodPost, AcceptPath, scan)
}

func (c *Client) Pause(ctx context.Context) (Paused, error) {
	return ask[Paused](ctx, c, http.MethodPost, PausePath, struct{}{})
}

func (c *Client) Resume(ctx context.Context) (Paused, error) {
	return ask[Paused](ctx, c, http.MethodPost, ResumePath, struct{}{})
}

func (c *Client) Executions(ctx context.Context) ([]Execution, error) {
	return ask[[]Execution](ctx, c, http.MethodGet, ExecutionsPath, nil)
}

func (c *Client) Logs(ctx context.Context, executionID string) ([]TimelineEntry, error) {
	return ask[[]TimelineEntry](ctx, c, http.MethodGet, forRun(LogsPath, executionID), nil)
}

func (c *Client) Disconnect(ctx context.Context) (Disconnected, error) {
	return ask[Disconnected](ctx, c, http.MethodPost, DisconnectPath, struct{}{})
}

func ask[T any](
	ctx context.Context,
	c *Client,
	method string,
	path string,
	payload any,
) (T, error) {
	var answer T

	// A caller that has already set a deadline meant it: asking a question is allowed to hold the
	// socket open for as long as the daemon holds the question, and the ordinary request timeout
	// would cut that short.
	if _, set := ctx.Deadline(); !set {
		patient, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
		defer cancel()

		ctx = patient
	}

	var body io.Reader

	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return answer, fmt.Errorf("encode the request for %s: %w", path, err)
		}

		body = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, "http://"+Host+path, body)
	if err != nil {
		return answer, fmt.Errorf("build the request for %s: %w", path, err)
	}

	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return answer, c.unreachable(err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return answer, refusal(response)
	}

	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		return answer, fmt.Errorf("read the runner's answer: %w", err)
	}

	return answer, nil
}

func (c *Client) unreachable(err error) error {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, fs.ErrNotExist) {
		return entity.Exit(entity.ExitDaemonUnavailable, fmt.Errorf(
			"%w on %s; start one with 'norn runner start'", entity.ErrDaemonUnavailable, c.path,
		))
	}

	return fmt.Errorf("reach the runner on %s: %w", c.path, err)
}

func refusal(response *http.Response) error {
	var failure Failure

	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil || failure.Message == "" {
		return fmt.Errorf("the runner answered %s", response.Status)
	}

	refused := errors.New(failure.Message)

	if failure.Reason == ReasonNotEnrolled {
		return entity.Exit(entity.ExitNotEnrolled, refused)
	}

	return refused
}

func (c *Client) Services(ctx context.Context, executionID string) ([]Service, error) {
	return ask[[]Service](ctx, c, http.MethodGet, forRun(ServicesPath, executionID), nil)
}

func (c *Client) StartService(
	ctx context.Context,
	executionID string,
	request ServiceRequest,
) (Service, error) {
	return ask[Service](ctx, c, http.MethodPost, forRun(ServicesPath, executionID), request)
}

func (c *Client) StopService(
	ctx context.Context,
	executionID string,
	name string,
) (Service, error) {
	path := forService(ServicePath, executionID, name)

	return ask[Service](ctx, c, http.MethodDelete, path, nil)
}

func (c *Client) RestartService(
	ctx context.Context,
	executionID string,
	name string,
) (Service, error) {
	path := forService(ServiceRestartPath, executionID, name)

	return ask[Service](ctx, c, http.MethodPost, path, struct{}{})
}

func (c *Client) ServiceLogs(
	ctx context.Context,
	executionID string,
	name string,
	query entity.LogQuery,
) (ServiceLines, error) {
	path := forService(ServiceLogsPath, executionID, name)

	asked := url.Values{}

	if query.Tail > 0 {
		asked.Set("tail", strconv.Itoa(query.Tail))
	}

	if query.Grep != "" {
		asked.Set("grep", query.Grep)
	}

	if len(asked) > 0 {
		path += "?" + asked.Encode()
	}

	return ask[ServiceLines](ctx, c, http.MethodGet, path, nil)
}

func (c *Client) AllocatePort(ctx context.Context, executionID string, name string) (Port, error) {
	path := forRun(PortsPath, executionID)

	return ask[Port](ctx, c, http.MethodPost, path, PortRequest{Name: name})
}

func (c *Client) Previews(ctx context.Context, executionID string) ([]Preview, error) {
	return ask[[]Preview](ctx, c, http.MethodGet, forRun(PreviewsPath, executionID), nil)
}

func (c *Client) ExposePreview(
	ctx context.Context,
	executionID string,
	request PreviewRequest,
) (Preview, error) {
	return ask[Preview](ctx, c, http.MethodPost, forRun(PreviewsPath, executionID), request)
}

func (c *Client) ClosePreview(
	ctx context.Context,
	executionID string,
	name string,
) (Preview, error) {
	path := strings.Replace(
		forRun(PreviewPath, executionID), "{preview}", url.PathEscape(name), 1,
	)

	return ask[Preview](ctx, c, http.MethodDelete, path, nil)
}

func (c *Client) Report(
	ctx context.Context,
	executionID string,
	request ProgressRequest,
) (ProgressRequest, error) {
	path := forRun(ProgressPath, executionID)

	return ask[ProgressRequest](ctx, c, http.MethodPost, path, request)
}

func (c *Client) PublishArtifact(
	ctx context.Context,
	executionID string,
	request ArtifactRequest,
) (Artifact, error) {
	path := forRun(ArtifactsPath, executionID)

	return ask[Artifact](ctx, c, http.MethodPost, path, request)
}

func (c *Client) Complete(
	ctx context.Context,
	executionID string,
	request CompleteRequest,
) (Completed, error) {
	path := forRun(CompletePath, executionID)

	return ask[Completed](ctx, c, http.MethodPost, path, request)
}

func (c *Client) RunStep(
	ctx context.Context,
	executionID string,
	request StepRequest,
) (StepResult, error) {
	return ask[StepResult](ctx, c, http.MethodPost, forRun(StepsPath, executionID), request)
}

// Ask holds the socket open for as long as the daemon will hold the question open, plus the time
// any other request is allowed. Giving up at the ordinary timeout would answer the caller with a
// deadline it could do nothing about, and an agent told that reasonably asks again.
func (c *Client) Ask(
	ctx context.Context,
	executionID string,
	request QuestionRequest,
) (QuestionAnswer, error) {
	patient, done := context.WithTimeout(ctx, c.waiting(request))
	defer done()

	return ask[QuestionAnswer](
		patient, c, http.MethodPost, forRun(QuestionsPath, executionID), request,
	)
}

func (c *Client) waiting(request QuestionRequest) time.Duration {
	if !request.Blocking {
		return c.cfg.RequestTimeout
	}

	held := c.questions.SoftWait

	if asked := time.Duration(request.WaitSeconds) * time.Second; asked > 0 && asked < c.questions.MaxWait {
		held = asked
	}

	return held + c.cfg.RequestTimeout
}

func forRun(path string, executionID string) string {
	return strings.Replace(path, "{executionId}", url.PathEscape(executionID), 1)
}

func forService(path string, executionID string, name string) string {
	return strings.Replace(forRun(path, executionID), "{service}", url.PathEscape(name), 1)
}
