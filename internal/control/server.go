package control

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

type Server struct {
	runner     config.Runner
	state      config.State
	app        config.App
	dir        *statedir.Dir
	enrolments service.Enrolments
	sessions   service.Sessions
	updates    service.Updates
	codebases  service.Codebases
	channels   service.Channels
	executions service.Executions
	services   service.Services
	questions  service.Questions
	previews   service.Previews
	uploads    service.Uploads
	tokens     repository.RunToken
	build      entity.Build
	startedAt  time.Time
	handler    http.Handler
}

func NewServer(
	runner config.Runner,
	state config.State,
	app config.App,
	dir *statedir.Dir,
	enrolments service.Enrolments,
	sessions service.Sessions,
	updates service.Updates,
	codebases service.Codebases,
	channels service.Channels,
	executions service.Executions,
	services service.Services,
	questions service.Questions,
	previews service.Previews,
	uploads service.Uploads,
	tokens repository.RunToken,
	build entity.Build,
) *Server {
	server := &Server{
		runner:     runner,
		state:      state,
		app:        app,
		dir:        dir,
		enrolments: enrolments,
		sessions:   sessions,
		updates:    updates,
		codebases:  codebases,
		channels:   channels,
		executions: executions,
		services:   services,
		questions:  questions,
		previews:   previews,
		uploads:    uploads,
		tokens:     tokens,
		build:      build,
		startedAt:  time.Now().UTC(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+StatusPath, server.status)
	mux.HandleFunc("GET "+VersionPath, server.version)
	mux.HandleFunc("POST "+ConnectPath, server.connect)
	mux.HandleFunc("POST "+DisconnectPath, server.disconnect)
	mux.HandleFunc("POST "+InspectPath, server.inspect)
	mux.HandleFunc("POST "+AcceptPath, server.accept)
	mux.HandleFunc("POST "+PausePath, server.pause)
	mux.HandleFunc("POST "+ResumePath, server.resume)
	mux.HandleFunc("GET "+ExecutionsPath, server.runs)
	mux.HandleFunc("GET "+LogsPath, server.logs)
	mux.HandleFunc("GET "+ServicesPath, server.guarded(server.runServices))
	mux.HandleFunc("POST "+ServicesPath, server.guarded(server.startService))
	mux.HandleFunc("DELETE "+ServicePath, server.guarded(server.stopService))
	mux.HandleFunc("POST "+ServiceRestartPath, server.guarded(server.restartService))
	mux.HandleFunc("GET "+ServiceLogsPath, server.guarded(server.serviceLogs))
	mux.HandleFunc("POST "+StepsPath, server.guarded(server.step))
	mux.HandleFunc("POST "+PortsPath, server.guarded(server.allocatePort))
	mux.HandleFunc("POST "+QuestionsPath, server.guarded(server.ask))
	mux.HandleFunc("GET "+PreviewsPath, server.guarded(server.runPreviews))
	mux.HandleFunc("POST "+PreviewsPath, server.guarded(server.exposePreview))
	mux.HandleFunc("DELETE "+PreviewPath, server.guarded(server.closePreview))
	mux.HandleFunc("POST "+ProgressPath, server.guarded(server.progress))
	mux.HandleFunc("POST "+ArtifactsPath, server.guarded(server.publishArtifact))
	mux.HandleFunc("POST "+CompletePath, server.guarded(server.complete))

	server.handler = recovering(mux)

	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	status := Status{
		Version:    s.app.Version,
		PID:        os.Getpid(),
		StartedAt:  s.startedAt,
		StateDir:   s.dir.Root(),
		ConfigFile: s.state.ConfigFile,
		Socket:     s.dir.Socket(),
		Server:     s.runner.Server,
		Capacity:   s.runner.Capacity,
		Runtime:    string(s.runner.Runtime),
		Session:    string(entity.SessionUnenrolled),
		Update:     updateOf(s.updates.Report()),
	}

	report := s.sessions.Report()
	status.Session = string(report.State)
	status.SessionDetail = report.Detail
	status.Expires = optionalTime(report.ExpiresAt)

	status.Codebases = s.heldCodebases(r)
	status.Channel = channelOf(s.channels.Report(r.Context()))
	status.Scheduler = s.schedulerOf(s.executions.Report(r.Context()))
	status.Driver = driverOf(s.executions.Driver(r.Context()))

	identity, err := s.enrolments.Current(r.Context())
	if err != nil {
		if !errors.Is(err, entity.ErrNotEnrolled) {
			status.SessionDetail = err.Error()
		}

		respond(w, r, http.StatusOK, status)

		return
	}

	status.Enrolled = true
	status.Agent = identity.Agent()
	status.Machine = identity.RunnerName
	status.RunnerID = identity.RunnerID.String()
	status.Store = string(identity.Store)

	respond(w, r, http.StatusOK, status)
}

func (s *Server) pause(w http.ResponseWriter, r *http.Request) {
	s.executions.Pause(r.Context())

	respond(w, r, http.StatusOK, Paused{Paused: true})
}

func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	s.executions.Resume(r.Context())
	s.channels.Wake()

	respond(w, r, http.StatusOK, Paused{Paused: false})
}

func channelOf(report entity.ChannelReport) Channel {
	return Channel{
		State:       string(report.State),
		Detail:      report.Detail,
		ConnectedAt: optionalTime(report.ConnectedAt),
		LastHeard:   optionalTime(report.LastHeard),
		Waiting:     report.Waiting,
	}
}

func driverOf(health entity.DriverHealth) Driver {
	return Driver{
		Kind:      string(health.Kind),
		Installed: health.Installed,
		Version:   health.Version,
		SignedIn:  health.SignedIn,
		Account:   health.Account,
		Problem:   health.Problem,
	}
}

func (s *Server) schedulerOf(report entity.SchedulerReport) Scheduler {
	held := Scheduler{
		Capacity:   report.Capacity,
		Used:       report.Used,
		Paused:     report.Paused,
		Watermark:  report.Room.Watermark,
		Retention:  retentionOf(report.Runs, s.runner.Retention),
		Executions: make([]Execution, 0, len(report.Executions)),
	}

	if report.Room.Known {
		free := report.Room.Free
		held.FreeDisk = &free
	}

	for _, execution := range report.Executions {
		held.Executions = append(held.Executions, s.executionOf(execution, true))
	}

	return held
}

func retentionOf(report entity.RunsReport, keeping config.Retention) Retention {
	return Retention{
		Runs:               report.Runs,
		Bytes:              report.Bytes,
		Budget:             keeping.RunsMaxDisk,
		WorkspaceAfterDone: keeping.WorkspaceAfterDone.String(),
		RunsMaxAge:         keeping.RunsMaxAge.String(),
		SweptAt:            optionalTime(report.SweptAt),
	}
}

func (s *Server) executionOf(execution entity.Execution, held bool) Execution {
	waiting := ""

	if question, stopped := s.questions.Waiting(execution.ID); stopped {
		waiting = question.Message
	}

	return Execution{
		ID:         execution.ID,
		Reference:  execution.Reference,
		IssueKey:   execution.IssueKey,
		Attempt:    execution.Attempt,
		Title:      execution.Title,
		State:      string(execution.State),
		Directory:  execution.Directory,
		Held:       held,
		AcceptedAt: execution.AcceptedAt,
		StartedAt:  optionalTime(execution.StartedAt),
		Lease:      optionalTime(execution.Lease),
		Waiting:    waiting,
	}
}

func (s *Server) runs(w http.ResponseWriter, r *http.Request) {
	found, err := s.executions.List(r.Context())
	if err != nil {
		status, reason, message := adviceFor(err)
		respond(w, r, status, Failure{Reason: reason, Message: message})

		return
	}

	holding := make(map[string]bool, len(found))

	for _, execution := range s.executions.Report(r.Context()).Executions {
		holding[execution.ID] = true
	}

	executions := make([]Execution, 0, len(found))

	for _, execution := range found {
		executions = append(executions, s.executionOf(execution, holding[execution.ID]))
	}

	respond(w, r, http.StatusOK, executions)
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	timeline, err := s.executions.Timeline(r.Context(), r.PathValue("executionId"))
	if err != nil {
		status, reason, message := adviceFor(err)
		respond(w, r, status, Failure{Reason: reason, Message: message})

		return
	}

	entries := make([]TimelineEntry, 0, len(timeline))

	for _, entry := range timeline {
		entries = append(entries, TimelineEntry{
			Kind:     string(entry.Kind),
			State:    string(entry.State),
			Reason:   entry.Reason,
			Occurred: entry.Occurred,
		})
	}

	respond(w, r, http.StatusOK, entries)
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	respond(w, r, http.StatusOK, buildOf(s.build, s.updates.Report()))
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	var request ConnectRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{Reason: ReasonRefused, Message: "that connect request is malformed"})

		return
	}

	connected, err := s.enrolments.Connect(r.Context(), service.ConnectInput{
		Token: request.Token,
		Name:  request.Name,
		Store: entity.Store(request.Store),
		Force: request.Force,
	})
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, Connected{
		Agent:         connected.Identity.Agent(),
		Machine:       connected.Identity.RunnerName,
		RunnerID:      connected.Identity.RunnerID.String(),
		Server:        connected.Identity.Server,
		Store:         string(connected.Identity.Store),
		Session:       string(connected.Session.State),
		SessionDetail: connected.Session.Detail,
		Expires:       optionalTime(connected.Session.ExpiresAt),
	})
}

func (s *Server) disconnect(w http.ResponseWriter, r *http.Request) {
	identity, err := s.enrolments.Disconnect(r.Context())
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, Disconnected{
		Agent:    identity.Agent(),
		Machine:  identity.RunnerName,
		RunnerID: identity.RunnerID.String(),
		Server:   identity.Server,
	})
}

func (s *Server) refuse(w http.ResponseWriter, r *http.Request, err error) {
	status, reason, message := adviceFor(err)

	logging.From(r.Context()).WarnContext(
		r.Context(),
		"the runner refused a control request",
		slog.String("path", r.URL.Path),
		slog.String("error", err.Error()),
	)

	respond(w, r, status, Failure{Reason: reason, Message: message})
}

func buildOf(build entity.Build, update entity.Update) Build {
	return Build{
		Version:     build.Version,
		Commit:      build.Commit,
		Modified:    build.Modified,
		CommittedAt: optionalTime(build.CommittedAt),
		OS:          build.OS,
		Arch:        build.Arch,
		Go:          build.Go,
		Update:      updateOf(update),
	}
}

func updateOf(update entity.Update) Update {
	return Update{
		State:  string(update.State),
		Latest: update.Latest,
		URL:    update.URL,
		Detail: update.Detail,
	}
}

func optionalTime(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}

	return &at
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
					Reason:  ReasonRefused,
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
