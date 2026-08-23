package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/mcpserver"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

const runID = "exec-01TOOLS"

type daemon struct {
	answers  map[string]any
	refusals map[string]control.Failure
	asked    map[string]json.RawMessage
}

func newDaemon() *daemon {
	return &daemon{
		answers:  map[string]any{},
		refusals: map[string]control.Failure{},
		asked:    map[string]json.RawMessage{},
	}
}

func (d *daemon) handler() http.Handler {
	mux := http.NewServeMux()

	for _, path := range []string{
		control.ServicesPath, control.ServicePath, control.ServiceRestartPath,
		control.ServiceLogsPath, control.StepsPath, control.PortsPath,
		control.QuestionsPath, control.PreviewsPath, control.PreviewPath,
		control.ProgressPath, control.ArtifactsPath, control.CompletePath,
	} {
		mux.HandleFunc(path, d.answer)
	}

	return mux
}

func (d *daemon) answer(w http.ResponseWriter, r *http.Request) {
	pattern := r.Pattern

	if body, err := readBody(r); err == nil {
		d.asked[pattern] = body
	}

	w.Header().Set("Content-Type", "application/json")

	if refusal, refused := d.refusals[pattern]; refused {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(refusal)

		return
	}

	answer, known := d.answers[pattern]
	if !known {
		answer = map[string]any{}
	}

	_ = json.NewEncoder(w).Encode(answer)
}

func readBody(r *http.Request) (json.RawMessage, error) {
	var held json.RawMessage

	if r.Body == nil {
		return nil, http.ErrBodyNotAllowed
	}

	if err := json.NewDecoder(r.Body).Decode(&held); err != nil {
		return nil, err
	}

	return held, nil
}

type harness struct {
	daemon  *daemon
	session *mcp.ClientSession
}

func newHarness(t *testing.T, made *daemon) *harness {
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

	daemonServer := &http.Server{Handler: made.handler(), ReadHeaderTimeout: time.Second}

	go func() { _ = daemonServer.Serve(listener) }()

	t.Cleanup(func() {
		_ = daemonServer.Close()
		cleanup()
	})

	client := control.NewClient(
		config.Control{DialTimeout: time.Second, RequestTimeout: 2 * time.Second},
		config.Questions{SoftWait: 50 * time.Millisecond, MaxWait: time.Second},
		dir,
		"a-token",
	)

	server := mcpserver.New(client, config.App{Version: "test"})

	forServer, forClient := mcp.NewInMemoryTransports()

	served := make(chan error, 1)

	go func() { served <- server.Serve(context.Background(), runID, forServer) }()

	session, err := mcp.NewClient(
		&mcp.Implementation{Name: "test", Version: "test"}, nil,
	).Connect(context.Background(), forClient, nil)
	if err != nil {
		t.Fatalf("connect to norn's tools: %v", err)
	}

	t.Cleanup(func() {
		_ = session.Close()
		<-served
	})

	return &harness{daemon: made, session: session}
}

func (h *harness) call(t *testing.T, name string, arguments any) *mcp.CallToolResult {
	t.Helper()

	result, err := h.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}

	return result
}

func (h *harness) tools(t *testing.T) []*mcp.Tool {
	t.Helper()

	listed, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list the tools: %v", err)
	}

	return listed.Tools
}

func decode(t *testing.T, result *mcp.CallToolResult, into any) {
	t.Helper()

	if result.IsError {
		t.Fatalf("the call was refused: %+v", result.Content)
	}

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("read what came back: %v", err)
	}

	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("read what came back: %v", err)
	}
}

func text(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	said := ""

	for _, content := range result.Content {
		if held, ok := content.(*mcp.TextContent); ok {
			said += held.Text
		}
	}

	return said
}
