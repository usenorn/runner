package update_test

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/entity"
)

func TestANewerReleaseIsReportedWithSomewhereToGoAndReadIt(t *testing.T) {
	h := newHarness(t, installed, true)
	h.publishes("v1.3.0")

	update := h.service.Check(context.Background())

	if update.State != entity.UpdateAvailable {
		t.Fatalf("a runner on %s told v1.3.0 exists reported %q", installed, update.State)
	}

	if update.Latest != "v1.3.0" {
		t.Errorf("the update does not name the version to take, only %q", update.Latest)
	}

	if update.URL == "" {
		t.Errorf("the update names no page, so there is nowhere to go and read what changed")
	}

	if update.CheckedAt.IsZero() {
		t.Errorf("the update is undated, so nobody can tell how stale the answer is")
	}
}

func TestARunnerOnTheNewestReleaseIsToldItIsCurrent(t *testing.T) {
	h := newHarness(t, installed, true)
	h.publishes(installed)

	if update := h.service.Check(context.Background()); update.State != entity.UpdateCurrent {
		t.Fatalf("a runner on the published release reported %q, want current", update.State)
	}
}

func TestADevelopmentBuildIsNeverNaggedAndNeverAsks(t *testing.T) {
	h := newHarness(t, entity.DevelopmentVersion, true)

	if update := h.service.Report(); update.State != entity.UpdateOff {
		t.Fatalf("a development build reports %q before checking, want the check switched off", update.State)
	}

	if update := h.service.Check(context.Background()); update.State != entity.UpdateOff {
		t.Fatalf("a development build checked anyway and reported %q", update.State)
	}
}

func TestSwitchingTheCheckOffMeansTheFeedIsNeverAsked(t *testing.T) {
	h := newHarness(t, installed, false)

	update := h.service.Check(context.Background())
	if update.State != entity.UpdateOff {
		t.Fatalf("the check reported %q with update.check false, want it off", update.State)
	}

	if update.Detail == "" {
		t.Fatalf("nothing says why no update is reported, so it looks like a failure")
	}
}

func TestAFeedThatGoesDownKeepsTheLastAnswerInsteadOfFlapping(t *testing.T) {
	h := newHarness(t, installed, true)

	gomock.InOrder(
		h.releases.EXPECT().
			Latest(gomock.Any()).
			Return(entity.Release{Version: "v1.3.0", URL: "https://example.invalid"}, nil),
		h.releases.EXPECT().
			Latest(gomock.Any()).
			Return(entity.Release{}, entity.ErrReleaseUnavailable),
	)

	if update := h.service.Check(context.Background()); update.State != entity.UpdateAvailable {
		t.Fatalf("the first check reported %q, want the newer release", update.State)
	}

	update := h.service.Check(context.Background())
	if update.State != entity.UpdateAvailable || update.Latest != "v1.3.0" {
		t.Fatalf(
			"an unreachable feed dropped the update already found and reported %q",
			update.State,
		)
	}

	if update.Detail == "" {
		t.Fatalf("nothing records that the last check failed, so the answer looks fresh")
	}
}

func TestAFeedThatHasNeverAnsweredIsReportedAsUnknownRatherThanCurrent(t *testing.T) {
	h := newHarness(t, installed, true)

	h.releases.EXPECT().
		Latest(gomock.Any()).
		Return(entity.Release{}, entity.ErrReleaseUnavailable)

	update := h.service.Check(context.Background())
	if update.State != entity.UpdateUnknown {
		t.Fatalf(
			"a check that has never succeeded reported %q, which claims this runner is up to date",
			update.State,
		)
	}
}

func TestAServerWithNoReleasesIsUnknownNotUpToDate(t *testing.T) {
	h := newHarness(t, installed, true)

	h.releases.EXPECT().
		Latest(gomock.Any()).
		Return(entity.Release{}, entity.ErrReleaseUnpublished)

	update := h.service.Check(context.Background())
	if update.State != entity.UpdateUnknown {
		t.Fatalf("a feed with no releases reported %q, want it left unknown", update.State)
	}
}
