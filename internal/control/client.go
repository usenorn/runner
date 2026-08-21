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
	"syscall"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type Client struct {
	http *http.Client
	cfg  config.Control
	path string
}

func NewClient(cfg config.Control, dir *statedir.Dir) *Client {
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
		cfg:  cfg,
		path: path,
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

	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()

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
