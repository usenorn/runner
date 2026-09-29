package mcpbridge_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/mcpbridge"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

const (
	runID    = "exec-01BRIDGE"
	runToken = "the-run-token"
)

type bearing struct {
	token string
	next  http.RoundTripper
}

func (b bearing) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = "host.docker.internal"
	r.Header.Set("Authorization", "Bearer "+b.token)

	return b.next.RoundTrip(r)
}

func newBridge(t *testing.T) *httptest.Server {
	t.Helper()

	root, err := os.MkdirTemp("/tmp", "nrn")
	if err != nil {
		t.Fatalf("create temporary root: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	listener, cleanup, err := socket.New(dir)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	norn := mcp.NewServer(&mcp.Implementation{Name: "norn", Version: "test"}, nil)
	mcp.AddTool(norn, &mcp.Tool{Name: "norn_get_issue", Description: "Fetch one issue."}, func(
		context.Context, *mcp.CallToolRequest, struct{},
	) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})

	tools := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return norn }, nil)
	mux := http.NewServeMux()
	mux.HandleFunc(control.NornToolsPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+runToken {
			http.Error(w, "wrong run token", http.StatusUnauthorized)

			return
		}

		tools.ServeHTTP(w, r)
	})

	daemon := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}

	go func() { _ = daemon.Serve(listener) }()

	t.Cleanup(func() {
		_ = daemon.Close()
		cleanup()
	})

	bridge, closeBridge := mcpbridge.New(
		config.Control{DialTimeout: time.Second, RequestTimeout: 2 * time.Second},
		config.Questions{SoftWait: 50 * time.Millisecond, MaxWait: time.Second},
		dir,
		config.App{Version: "test"},
	)

	served := httptest.NewServer(bridge)

	t.Cleanup(func() {
		served.Close()
		closeBridge()
	})

	return served
}

func connect(served *httptest.Server, token string) (*mcp.ClientSession, error) {
	return mcp.NewClient(&mcp.Implementation{Name: "claude", Version: "test"}, nil).Connect(
		context.Background(),
		&mcp.StreamableClientTransport{
			Endpoint:   served.URL + "/executions/" + runID + "/mcp",
			HTTPClient: &http.Client{Transport: bearing{token: token, next: http.DefaultTransport}},
		},
		nil,
	)
}

func TestAnAgentInAContainerReachesItsRunsToolsWithTheRunsToken(t *testing.T) {
	served := newBridge(t)

	session, err := connect(served, runToken)
	if err != nil {
		t.Fatalf(
			"the agent could not reach its tools from inside its container: %v. Without them "+
				"it cannot start a service, ask a person or say it is finished",
			err,
		)
	}

	t.Cleanup(func() { _ = session.Close() })

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list the tools: %v", err)
	}

	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}

	for _, want := range []string{"start_service", "ask_human", "complete_task", "norn_get_issue"} {
		if !slices.Contains(names, want) {
			t.Fatalf("the agent's tools are %v, and %s is missing", names, want)
		}
	}
}

func TestSomethingWithoutTheRunsTokenIsTurnedAwayFromItsTools(t *testing.T) {
	served := newBridge(t)

	for _, token := range []string{"", "another-runs-token"} {
		if session, err := connect(served, token); err == nil {
			_ = session.Close()

			t.Fatalf(
				"a caller holding %q was let into this run's tools. Every container on the "+
					"machine can reach this port, so the token is all that keeps one run out of "+
					"another's services and questions",
				token,
			)
		}
	}
}
