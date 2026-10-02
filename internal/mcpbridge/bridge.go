package mcpbridge

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/mcpserver"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

const (
	bearerPrefix = "Bearer "
	idleLimit    = 10 * time.Minute
)

type served struct {
	server       *mcp.Server
	handler      http.Handler
	closeBackend func()
	used         time.Time
}

type Bridge struct {
	control   config.Control
	questions config.Questions
	steps     config.Supervisor
	dir       *statedir.Dir
	app       config.App
	lifetime  context.Context
	now       func() time.Time

	mu   sync.Mutex
	held map[string]*served
	mux  *http.ServeMux
}

func New(
	control config.Control,
	questions config.Questions,
	steps config.Supervisor,
	dir *statedir.Dir,
	app config.App,
) (*Bridge, func()) {
	lifetime, stop := context.WithCancel(context.Background())

	bridge := &Bridge{
		control:   control,
		questions: questions,
		steps:     steps,
		dir:       dir,
		app:       app,
		lifetime:  lifetime,
		now:       time.Now,
		held:      map[string]*served{},
		mux:       http.NewServeMux(),
	}

	bridge.mux.HandleFunc(entity.RunToolsRoute, bridge.serve)

	return bridge, func() {
		stop()
		bridge.close()
	}
}

func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mux.ServeHTTP(w, r)
}

func (b *Bridge) serve(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), bearerPrefix))
	if token == "" || !strings.HasPrefix(r.Header.Get("Authorization"), bearerPrefix) {
		http.Error(w, "this run's tools need the run's own token", http.StatusUnauthorized)

		return
	}

	executionID := r.PathValue("executionId")

	held, err := b.serverFor(executionID, token)
	if err != nil {
		logging.From(r.Context()).WarnContext(
			r.Context(),
			"a container asked for a run's tools this runner would not open",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)

		http.Error(w, "norn would not open this run's tools for that token", http.StatusUnauthorized)

		return
	}

	held.handler.ServeHTTP(w, r)
}

func (b *Bridge) serverFor(executionID, token string) (*served, error) {
	key := executionID + "\n" + token

	b.mu.Lock()
	defer b.mu.Unlock()

	b.prune(key)

	if held, found := b.held[key]; found {
		held.used = b.now()

		return held, nil
	}

	client := control.NewClient(b.control, b.questions, b.steps, b.dir, control.Bearer(token))

	server, closeBackend, err := mcpserver.New(client, b.app).Build(b.lifetime, executionID)
	if err != nil {
		return nil, err
	}

	held := &served{
		server: server,
		handler: mcp.NewStreamableHTTPHandler(
			func(*http.Request) *mcp.Server { return server },
			&mcp.StreamableHTTPOptions{DisableLocalhostProtection: true, SessionTimeout: idleLimit},
		),
		closeBackend: closeBackend,
		used:         b.now(),
	}

	b.held[key] = held

	return held, nil
}

func (b *Bridge) prune(keep string) {
	for key, held := range b.held {
		if key == keep || b.now().Sub(held.used) < idleLimit || live(held.server) {
			continue
		}

		held.closeBackend()
		delete(b.held, key)
	}
}

func live(server *mcp.Server) bool {
	for range server.Sessions() {
		return true
	}

	return false
}

func (b *Bridge) close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for key, held := range b.held {
		held.closeBackend()
		delete(b.held, key)
	}
}
