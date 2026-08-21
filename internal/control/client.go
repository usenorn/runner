package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+Host+StatusPath, nil)
	if err != nil {
		return Status{}, fmt.Errorf("build status request: %w", err)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return Status{}, c.unreachable(err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return Status{}, refusal(response)
	}

	var status Status

	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return Status{}, fmt.Errorf("read the runner's answer: %w", err)
	}

	return status, nil
}

func (c *Client) unreachable(err error) error {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, fs.ErrNotExist) {
		return entity.Exit(entity.ExitDaemonUnavailable, fmt.Errorf(
			"no runner is listening on %s; start one with 'norn runner start'", c.path,
		))
	}

	return fmt.Errorf("reach the runner on %s: %w", c.path, err)
}

func refusal(response *http.Response) error {
	var failure Failure

	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil || failure.Message == "" {
		return fmt.Errorf("the runner answered %s", response.Status)
	}

	return fmt.Errorf("the runner refused: %s", failure.Message)
}
