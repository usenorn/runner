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

func TestAnAddressTheAgentWroteNeverReachesTheForge(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 4, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")
	h.pushes()

	asked := h.opens(t)

	h.publish(t, "Fixed the retry bug. Ask rae@northwind.co if the numbers look wrong.")

	if strings.Contains(asked.Body, "rae@northwind.co") {
		t.Fatalf(
			"a personal address reached a pull request description:\n%s\nOnce it is on a remote "+
				"it is public, and taking it down does not take it out of anyone's inbox",
			asked.Body,
		)
	}

	if !strings.Contains(asked.Body, "Fixed the retry bug.") {
		t.Fatalf(
			"the summary went with the address:\n%s\nA reviewer must not lose what the run did "+
				"over one bad word",
			asked.Body,
		)
	}
}

func TestTheRunSaysWhatItTookOutOfThePullRequest(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 4, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000002")
	h.pushes()
	h.opens(t)

	h.publish(t, "Added the helper.\n\nCo-Authored-By: Rae Chen <rae@northwind.co>")

	if !h.noted(t, "was taken out before the pull request was opened") {
		t.Fatal(
			"nothing on the timeline says anything was removed, so the description silently " +
				"differs from what the coding agent wrote and nobody can tell why",
		)
	}
}

func TestAPullRequestNamesThePreviewTheRunLeftRunning(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 4, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000003")
	h.pushes()
	h.previews = []entity.PreviewLink{
		{Name: "web", Address: "https://web-exec-01abc.norn.ink/"},
	}

	asked := h.opens(t)

	h.publish(t, "added a median helper")

	if !strings.Contains(asked.Body, "https://web-exec-01abc.norn.ink/") {
		t.Fatalf(
			"the pull request does not say where the change is running:\n%s\nA reviewer wants to "+
				"look at it, not only read the diff",
			asked.Body,
		)
	}
}

func TestARunThatOpenedNoPreviewSaysNothingAboutOne(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 4, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000004")
	h.pushes()

	asked := h.opens(t)

	h.publish(t, "added a median helper")

	if strings.Contains(asked.Body, "Preview:") {
		t.Fatalf("the pull request carries an empty preview heading:\n%s", asked.Body)
	}
}
