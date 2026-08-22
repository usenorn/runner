package supervisor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

const (
	loopback   = "127.0.0.1"
	dialing    = 2 * time.Second
	requesting = 5 * time.Second
)

func probe(ctx context.Context, health entity.Health, port int, lines <-chan string) error {
	switch health.Kind {
	case entity.HealthHTTP:
		return reachable(ctx, health.Path, port)
	case entity.HealthTCP:
		return connectable(ctx, port)
	case entity.HealthLog:
		return wrote(ctx, health.Pattern, lines)
	case entity.HealthNone:
		return nil
	default:
		return fmt.Errorf("%w: %q", entity.ErrServiceInvalid, health.Kind)
	}
}

func reachable(ctx context.Context, path string, port int) error {
	if path == "" {
		path = "/"
	}

	ctx, stop := context.WithTimeout(ctx, requesting)
	defer stop()

	address := "http://" + net.JoinHostPort(loopback, strconv.Itoa(port)) + path

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("ask %s: %w", address, err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("ask %s: %w", address, err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s answered %s", address, response.Status)
	}

	return nil
}

func connectable(ctx context.Context, port int) error {
	address := net.JoinHostPort(loopback, strconv.Itoa(port))

	dialer := &net.Dialer{Timeout: dialing}

	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("reach %s: %w", address, err)
	}

	return connection.Close()
}

func wrote(ctx context.Context, pattern string, lines <-chan string) error {
	looking, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", entity.ErrServiceInvalid, pattern, err)
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for a line matching %s: %w", pattern, ctx.Err())
		case line, open := <-lines:
			if !open {
				return fmt.Errorf("nothing more was written while waiting for %s", pattern)
			}

			if looking.MatchString(line) {
				return nil
			}
		}
	}
}
