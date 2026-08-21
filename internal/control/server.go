package control

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type Server struct {
	runner    config.Runner
	state     config.State
	app       config.App
	dir       *statedir.Dir
	startedAt time.Time
	handler   http.Handler
}

func NewServer(
	runner config.Runner,
	state config.State,
	app config.App,
	dir *statedir.Dir,
) *Server {
	server := &Server{
		runner:    runner,
		state:     state,
		app:       app,
		dir:       dir,
		startedAt: time.Now().UTC(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+StatusPath, server.status)

	server.handler = recovering(mux)

	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	respond(w, r, http.StatusOK, Status{
		Version:    s.app.Version,
		PID:        os.Getpid(),
		StartedAt:  s.startedAt,
		StateDir:   s.dir.Root(),
		ConfigFile: s.state.ConfigFile,
		Socket:     s.dir.Socket(),
		Server:     s.runner.Server,
		Capacity:   s.runner.Capacity,
		Runtime:    string(s.runner.Runtime),
		Enrolled:   s.dir.Enrolled(),
	})
}

func recovering(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logging.From(r.Context()).ErrorContext(
					r.Context(),
					"control request panicked",
					slog.String("path", r.URL.Path),
					slog.Any("panic", recovered),
				)

				respond(w, r, http.StatusInternalServerError, Failure{
					Message: "the runner could not answer that",
				})
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func respond(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		logging.From(r.Context()).WarnContext(
			r.Context(), "writing a control response failed", slog.String("error", err.Error()),
		)
	}
}
