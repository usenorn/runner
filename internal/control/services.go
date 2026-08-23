package control

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func (s *Server) runServices(w http.ResponseWriter, r *http.Request) {
	records, err := s.services.List(r.Context(), r.PathValue("executionId"))
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, servicesOf(records))
}

func (s *Server) startService(w http.ResponseWriter, r *http.Request) {
	var request ServiceRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that service request is malformed",
		})

		return
	}

	record, err := s.services.Start(r.Context(), r.PathValue("executionId"), entity.Service{
		Name:        request.Name,
		Dir:         request.Dir,
		Command:     request.Command,
		Environment: request.Environment,
		Requires:    request.Requires,
		Health: entity.Health{
			Kind:    entity.HealthKind(request.Health.Kind),
			Path:    request.Health.Path,
			Port:    request.Health.Port,
			Pattern: request.Health.Pattern,
		},
	})
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, serviceOf(record))
}

func (s *Server) stopService(w http.ResponseWriter, r *http.Request) {
	record, err := s.services.Stop(
		r.Context(), r.PathValue("executionId"), r.PathValue("service"),
	)
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, serviceOf(record))
}

func (s *Server) restartService(w http.ResponseWriter, r *http.Request) {
	record, err := s.services.Restart(
		r.Context(), r.PathValue("executionId"), r.PathValue("service"),
	)
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, serviceOf(record))
}

func (s *Server) serviceLogs(w http.ResponseWriter, r *http.Request) {
	tail, err := strconv.Atoi(r.URL.Query().Get("tail"))
	if err != nil {
		tail = 0
	}

	lines, err := s.services.Logs(
		r.Context(), r.PathValue("executionId"), r.PathValue("service"),
		entity.LogQuery{Tail: tail, Grep: r.URL.Query().Get("grep")},
	)
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, ServiceLines{Lines: lines})
}

func (s *Server) step(w http.ResponseWriter, r *http.Request) {
	var request StepRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that step request is malformed",
		})

		return
	}

	timeout, err := time.ParseDuration(request.Timeout)
	if request.Timeout != "" && err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason:  ReasonRefused,
			Message: "that step asks for a timeout this machine cannot read: " + request.Timeout,
		})

		return
	}

	result, err := s.services.Step(r.Context(), r.PathValue("executionId"), entity.Step{
		Name:    request.Name,
		Dir:     request.Dir,
		Command: request.Command,
		Timeout: timeout,
	})
	if err != nil && !result.TimedOut {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, StepResult{
		Name:     result.Name,
		ExitCode: result.ExitCode,
		Output:   result.Output,
		Took:     result.Took.Round(time.Millisecond).String(),
		TimedOut: result.TimedOut,
	})
}

func (s *Server) allocatePort(w http.ResponseWriter, r *http.Request) {
	var request PortRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that port request is malformed",
		})

		return
	}

	port, err := s.services.Port(r.Context(), r.PathValue("executionId"), request.Name)
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, Port{Name: request.Name, Port: port})
}

func servicesOf(records []entity.ServiceRecord) []Service {
	services := make([]Service, 0, len(records))

	for _, record := range records {
		services = append(services, serviceOf(record))
	}

	return services
}

func serviceOf(record entity.ServiceRecord) Service {
	return Service{
		Name:      record.Name,
		Command:   record.Command,
		Dir:       record.Dir,
		Port:      record.Port,
		PID:       record.PID,
		State:     string(record.State),
		Attempts:  record.Attempts,
		Reason:    record.Reason,
		StartedAt: optionalTime(record.StartedAt),
		ChangedAt: optionalTime(record.ChangedAt),
	}
}
