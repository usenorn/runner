package control

import (
	"encoding/json"
	"net/http"

	"github.com/usenorn/runner/internal/entity"
)

func (s *Server) runPreviews(w http.ResponseWriter, r *http.Request) {
	open, err := s.previews.List(r.Context(), r.PathValue("executionId"))
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, previewsOf(open))
}

func (s *Server) exposePreview(w http.ResponseWriter, r *http.Request) {
	var request PreviewRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that preview request is malformed",
		})

		return
	}

	exposed, err := s.previews.Expose(r.Context(), r.PathValue("executionId"), entity.Preview{
		Name:    request.Name,
		Service: request.Service,
		Path:    request.Path,
	})
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, previewOf(exposed))
}

func (s *Server) closePreview(w http.ResponseWriter, r *http.Request) {
	closed, err := s.previews.Close(
		r.Context(), r.PathValue("executionId"), r.PathValue("preview"),
	)
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, previewOf(closed))
}

func previewsOf(open []entity.Preview) []Preview {
	previews := make([]Preview, 0, len(open))

	for _, preview := range open {
		previews = append(previews, previewOf(preview))
	}

	return previews
}

func previewOf(preview entity.Preview) Preview {
	return Preview{
		Name:      preview.Name,
		Service:   preview.Service,
		Path:      preview.Path,
		Port:      preview.Port,
		URL:       preview.URL,
		ExposedAt: preview.ExposedAt,
	}
}
