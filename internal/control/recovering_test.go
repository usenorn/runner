package control

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPanickingHandlerBecomesAFailureInsteadOfKillingTheDaemon(t *testing.T) {
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("a handler blew up")
	})

	recorder := httptest.NewRecorder()

	guarded := recovering(panicking)

	guarded.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, StatusPath, nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("a panicking handler answered %d, want 500", recorder.Code)
	}
}

func TestAHandlerThatAnswersIsLeftAlone(t *testing.T) {
	recorder := httptest.NewRecorder()

	guarded := recovering(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	guarded.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, StatusPath, nil))

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("the wrapper changed a healthy answer to %d", recorder.Code)
	}
}
