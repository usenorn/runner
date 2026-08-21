package dashboardclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	api "github.com/usenorn/norn/pkg/http/v1/dashboard"

	"github.com/usenorn/runner/internal/config"
)

const apiBasePath = "/v1"

type Client struct {
	*api.ClientWithResponses
}

func New(runner config.Runner, session config.Session, app config.App) (*Client, error) {
	base := strings.TrimSuffix(runner.Server, "/") + apiBasePath

	inner, err := api.NewClientWithResponses(
		base,
		api.WithHTTPClient(&http.Client{Timeout: session.RequestTimeout}),
		api.WithRequestEditorFn(userAgent(app.Version)),
	)
	if err != nil {
		return nil, fmt.Errorf("build a client for %s: %w", base, err)
	}

	return &Client{ClientWithResponses: inner}, nil
}

func userAgent(version string) api.RequestEditorFn {
	agent := "norn-runner/" + version

	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("User-Agent", agent)

		return nil
	}
}
