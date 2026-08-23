package control

import (
	"errors"
	"net/http"
	"strings"

	"github.com/usenorn/runner/internal/entity"
)

const bearerPrefix = "Bearer "

var errNoRunToken = errors.New(
	"this call acts on one run and carries nothing that says it belongs to it; " +
		entity.ExecutionTokenVariable + " is set inside a run and is what proves that",
)

var errWrongRunToken = errors.New(
	"what this call carries belongs to a different run, or to one that has already finished",
)

func (s *Server) guarded(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := presented(r)

		if token == "" {
			s.refuse(w, r, errNoRunToken)

			return
		}

		if !s.tokens.Allows(r.Context(), r.PathValue("executionId"), token) {
			s.refuse(w, r, errWrongRunToken)

			return
		}

		next(w, r)
	}
}

func presented(r *http.Request) string {
	held := r.Header.Get("Authorization")

	if !strings.HasPrefix(held, bearerPrefix) {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(held, bearerPrefix))
}
