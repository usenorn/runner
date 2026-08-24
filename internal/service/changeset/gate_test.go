package changeset_test

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/entity"
)

func (h *harness) opens(t *testing.T) *entity.PullRequest {
	t.Helper()

	h.forges.EXPECT().
		Available(gomock.Any(), gomock.Any()).
		Return(entity.ForgeGitHub, true).
		AnyTimes()
	h.forges.EXPECT().
		Existing(gomock.Any(), gomock.Any(), gomock.Any()).
		Return("", nil).
		AnyTimes()

	asked := &entity.PullRequest{}

	h.forges.EXPECT().
		Open(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request entity.PullRequest) (string, error) {
			*asked = request

			return "https://github.com/acme/api/pull/7", nil
		}).
		AnyTimes()

	return asked
}

func TestAPullRequestCarriesNoDescription(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 4, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")
	h.pushes()

	asked := h.opens(t)

	h.publish(t, "Fixed the retry bug. Ask rae@northwind.co if the numbers look wrong.")

	if asked.Body != "" {
		t.Fatalf(
			"a pull request opened with a description:\n%s\nThe diff and the commits say what "+
				"changed, and a reviewer did not ask for the coding agent's account of it",
			asked.Body,
		)
	}
}

func TestAnAddressInAnIssueTitleNeverReachesTheForge(t *testing.T) {
	h := newHarness(t, defaults())
	h.execution.Title = "Bounce mail to rae@northwind.co"
	h.changed(1, entity.Diffstat{Additions: 4, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000002")
	h.pushes()

	asked := h.opens(t)

	h.publish(t, "added a median helper")

	if strings.Contains(asked.Title, "rae@northwind.co") {
		t.Fatalf(
			"a personal address reached a pull request title:\n%s\nOnce it is on a remote it is "+
				"public, and taking it down does not take it out of anyone's inbox",
			asked.Title,
		)
	}

	if !strings.Contains(asked.Title, "NORN-54") {
		t.Fatalf(
			"the issue key went with the address:\n%s\nA reviewer must not lose which issue the "+
				"branch is for over one bad word",
			asked.Title,
		)
	}
}

func TestTheRunSaysWhatItTookOutOfThePullRequest(t *testing.T) {
	h := newHarness(t, defaults())
	h.execution.Title = "Bounce mail to rae@northwind.co"
	h.changed(1, entity.Diffstat{Additions: 4, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000003")
	h.pushes()
	h.opens(t)

	h.publish(t, "added a median helper")

	if !h.noted(t, "was taken out before the pull request was opened") {
		t.Fatal(
			"nothing on the timeline says anything was removed, so the title silently differs " +
				"from the issue's and nobody can tell why",
		)
	}
}
