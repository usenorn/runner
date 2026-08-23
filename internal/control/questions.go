package control

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	var request QuestionRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respond(w, r, http.StatusBadRequest, Failure{
			Reason: ReasonRefused, Message: "that question is malformed",
		})

		return
	}

	kind := entity.QuestionKind(request.Kind)
	if kind == "" {
		kind = entity.QuestionDecision
	}

	asked, err := s.questions.Ask(r.Context(), r.PathValue("executionId"), entity.Question{
		Kind:          kind,
		Blocking:      request.Blocking,
		Message:       request.Message,
		Options:       request.Options,
		AllowFreeText: request.AllowFreeText,
		Default:       request.Default,
		Wait:          time.Duration(request.WaitSeconds) * time.Second,
		Context: entity.QuestionContext{
			Preview: request.Preview,
			Files:   request.Files,
		},
	})
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	respond(w, r, http.StatusOK, QuestionAnswer{
		Status:     string(asked.Outcome),
		QuestionID: asked.Ref,
		Answer:     asked.Answer,
		AnsweredBy: asked.AnsweredBy,
		Advice:     asked.Advice,
	})
}
