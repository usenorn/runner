package control

import (
	"encoding/json"
	"net/http"

	"github.com/usenorn/runner/internal/entity"
)

func (s *Server) runPreviews(w http.ResponseWriter, r *http.Request) {
	execution, err := s.executions.Get(r.Context(), r.PathValue("executionId"))
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	open, err := s.previews.List(r.Context(), execution.ID)
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, s.previewsOf(execution, open))
}

func (s *Server) exposePreview(w http.ResponseWriter, r *http.Request) {
	var request PreviewRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that preview request is malformed",
		})

		return
	}

	execution, err := s.executions.Get(r.Context(), r.PathValue("executionId"))
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	exposed, err := s.previews.Expose(r.Context(), execution.ID, entity.Preview{
		Name:    request.Name,
		Service: request.Service,
		Path:    request.Path,
	})
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, s.previewOf(execution, exposed))
}

func (s *Server) closePreview(w http.ResponseWriter, r *http.Request) {
	execution, err := s.executions.Get(r.Context(), r.PathValue("executionId"))
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	closed, err := s.previews.Close(r.Context(), execution.ID, r.PathValue("preview"))
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, s.previewOf(execution, closed))
}

func (s *Server) previewsOf(execution entity.Execution, open []entity.Preview) []Preview {
	previews := make([]Preview, 0, len(open))

	for _, preview := range open {
		previews = append(previews, s.previewOf(execution, preview))
	}

	return previews
}

func (s *Server) previewOf(execution entity.Execution, preview entity.Preview) Preview {
	serving := s.sessions.Previews()
	shared := serving.Address(execution, preview.Port, preview.Path)
	live := s.tunnels.Report().State == entity.TunnelLive

	return Preview{
		Name:      preview.Name,
		Service:   preview.Service,
		Path:      preview.Path,
		Port:      preview.Port,
		URL:       preview.URL,
		Shared:    shared,
		Reach:     reachOf(shared, live),
		ExposedAt: preview.ExposedAt,
	}
}

func reachOf(shared string, live bool) string {
	switch {
	case shared == "":
		return "this machine only; this norn serves no preview domain"
	case !live:
		return "this machine only until this machine's tunnel to norn's gateway is back; " +
			shared + " will reach it then"
	default:
		return "anybody in the workspace who can see the issue, at " + shared
	}
}
