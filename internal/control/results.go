package control

import (
	"encoding/json"
	"net/http"

	"github.com/usenorn/runner/internal/entity"
)

func (s *Server) progress(w http.ResponseWriter, r *http.Request) {
	var request ProgressRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that progress report is malformed",
		})

		return
	}

	err := s.executions.Progress(r.Context(), r.PathValue("executionId"), entity.Progress{
		Summary: request.Summary,
		Phase:   request.Phase,
		Percent: request.Percent,
	})
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, request)
}

func (s *Server) publishArtifact(w http.ResponseWriter, r *http.Request) {
	var request ArtifactRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that artifact request is malformed",
		})

		return
	}

	if request.Label == "" {
		request.Label = entity.ArtifactLabelFor(request.Path)
	}

	receipt, err := s.uploads.Publish(r.Context(), r.PathValue("executionId"), entity.Artifact{
		Path:  request.Path,
		Label: request.Label,
	})
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, Artifact{
		ArtifactID: receipt.ID,
		Label:      receipt.Label,
		Bytes:      receipt.Bytes,
	})
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	var request CompleteRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that completion is malformed",
		})

		return
	}

	err := s.executions.Complete(r.Context(), r.PathValue("executionId"), entity.Completion{
		Summary: request.Summary,
		Notes:   request.Notes,
	})
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, Completed{Advice: entity.CompletedAdvice})
}
