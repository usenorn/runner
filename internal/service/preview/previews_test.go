package preview_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestAPreviewOpensOnTheServiceItNamesAtThePortNornGaveIt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.running(t, "exec-01OPEN", serving("web", 43111, entity.ServiceHealthy))

	exposed, err := h.service.Expose(ctx, "exec-01OPEN", entity.Preview{
		Service: "web",
		Path:    "/admin",
	})
	if err != nil {
		t.Fatalf("open a healthy service of this run: %v", err)
	}

	if exposed.Port != 43111 {
		t.Fatalf(
			"the preview opened on port %d, not the %d norn reserved for that service. A "+
				"caller does not choose the port and could not be trusted to",
			exposed.Port, 43111,
		)
	}

	if !strings.HasSuffix(exposed.URL, ":43111/admin") {
		t.Fatalf("the address was %q, which does not lead to the service and its path", exposed.URL)
	}

	if exposed.Name != "web" {
		t.Fatalf("a preview given no name of its own was called %q, not after its service", exposed.Name)
	}
}

func TestAPreviewCannotBeOpenedOnAnythingThisRunIsNotRunning(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.running(t, "exec-01OWNED", serving("web", 43111, entity.ServiceHealthy))

	_, err := h.service.Expose(ctx, "exec-01OWNED", entity.Preview{Service: "postgres"})
	if !errors.Is(err, entity.ErrPreviewNotOwned) {
		t.Fatalf(
			"a preview was opened on %q, which this run's supervisor never started: %v. There "+
				"is meant to be no path from a tool call to a port norn did not hand out, and "+
				"a database on this machine is exactly what would be reached through one",
			"postgres", err,
		)
	}
}

func TestAPreviewWaitsForTheServiceToBeHealthyRatherThanShowingAPersonNothing(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.running(t, "exec-01SICK", serving("web", 43111, entity.ServiceStarting))

	_, err := h.service.Expose(ctx, "exec-01SICK", entity.Preview{Service: "web"})
	if !errors.Is(err, entity.ErrPreviewNotOwned) {
		t.Fatalf(
			"a service that is still starting was opened for a person to look at: %v. They "+
				"follow the link, get nothing, and report it as broken",
			err,
		)
	}

	if !strings.Contains(err.Error(), string(entity.ServiceStarting)) {
		t.Fatalf("the refusal was %q and never says what the service is actually doing", err)
	}
}

func TestClosingAPreviewGivesTheNameBackAndListingShowsWhatIsOpen(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.running(
		t, "exec-01LIST",
		serving("web", 43111, entity.ServiceHealthy),
		serving("api", 43112, entity.ServiceHealthy),
	)

	for _, service := range []string{"web", "api"} {
		if _, err := h.service.Expose(ctx, "exec-01LIST", entity.Preview{
			Service: service,
		}); err != nil {
			t.Fatalf("open %s: %v", service, err)
		}
	}

	open, err := h.service.List(ctx, "exec-01LIST")
	if err != nil {
		t.Fatalf("list what is open: %v", err)
	}

	if len(open) != 2 || open[0].Name != "api" || open[1].Name != "web" {
		t.Fatalf("what is open came back as %+v, want api and web in a settled order", open)
	}

	if _, err := h.service.Close(ctx, "exec-01LIST", "web"); err != nil {
		t.Fatalf("close a preview: %v", err)
	}

	if _, err := h.service.Close(ctx, "exec-01LIST", "web"); !errors.Is(
		err, entity.ErrPreviewUnknown,
	) {
		t.Fatalf("closing something already closed answered %v, want it said so plainly", err)
	}

	open, err = h.service.List(ctx, "exec-01LIST")
	if err != nil {
		t.Fatalf("list what is open: %v", err)
	}

	if len(open) != 1 || open[0].Name != "api" {
		t.Fatalf("a closed preview is still listed as open: %+v", open)
	}
}

func TestTearingDownARunTakesItsPreviewsWithIt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.running(t, "exec-01GONE", serving("web", 43111, entity.ServiceHealthy))

	if _, err := h.service.Expose(ctx, "exec-01GONE", entity.Preview{Service: "web"}); err != nil {
		t.Fatalf("open a preview: %v", err)
	}

	h.service.Release(ctx, "exec-01GONE")

	open, err := h.service.List(ctx, "exec-01GONE")
	if err != nil {
		t.Fatalf("list what is open: %v", err)
	}

	if len(open) != 0 {
		t.Fatalf(
			"a torn-down run still holds %+v. Its services are stopped and its ports are back, "+
				"so every one of these addresses leads somewhere else now",
			open,
		)
	}
}

func TestOpeningAPreviewPutsItOnTheRunsOwnTimeline(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.running(t, "exec-01TOLD", serving("web", 43111, entity.ServiceHealthy))

	if _, err := h.service.Expose(ctx, "exec-01TOLD", entity.Preview{Service: "web"}); err != nil {
		t.Fatalf("open a preview: %v", err)
	}

	timeline, err := h.runs.Timeline(ctx, "exec-01TOLD")
	if err != nil {
		t.Fatalf("read the run's timeline: %v", err)
	}

	if len(timeline) != 1 || timeline[0].Kind != "preview" {
		t.Fatalf(
			"the run's timeline holds %+v. Somebody reading the issue has to be able to see "+
				"that there is something to look at, and where",
			timeline,
		)
	}

	if !strings.Contains(timeline[0].Reason, "43111") {
		t.Fatalf("the timeline line %q never says where to go", timeline[0].Reason)
	}

	spooled, err := h.spool.Count(ctx)
	if err != nil {
		t.Fatalf("count what is waiting for norn: %v", err)
	}

	if spooled != 1 {
		t.Fatalf("norn was sent %d messages about a preview being opened, want exactly one", spooled)
	}
}

func TestARunCannotHoldMorePreviewsThanNornWillShow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	records := make([]entity.ServiceRecord, 0, entity.PreviewsMax+1)

	for index := range entity.PreviewsMax + 1 {
		records = append(records, serving(
			string(rune('a'+index)), 43100+index, entity.ServiceHealthy,
		))
	}

	h.running(t, "exec-01MANY", records...)

	for index, record := range records {
		_, err := h.service.Expose(ctx, "exec-01MANY", entity.Preview{Service: record.Name})

		if index < entity.PreviewsMax && err != nil {
			t.Fatalf("open preview %d of %d: %v", index+1, entity.PreviewsMax, err)
		}

		if index == entity.PreviewsMax && !errors.Is(err, entity.ErrPreviewCrowded) {
			t.Fatalf(
				"a run opened preview %d and nothing stopped it: %v. Every one of these is a "+
					"route into this machine that stays open until the run ends",
				index+1, err,
			)
		}
	}
}

func TestAPreviewIsTimedTheWayEveryOtherLineOnTheTimelineIs(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.running(t, "exec-01WHEN", serving("web", 43111, entity.ServiceHealthy))

	exposed, err := h.service.Expose(ctx, "exec-01WHEN", entity.Preview{Service: "web"})
	if err != nil {
		t.Fatalf("open a preview: %v", err)
	}

	if _, offset := exposed.ExposedAt.Zone(); offset != 0 {
		t.Fatalf(
			"a preview was stamped in this machine's own time zone. Every other line a run "+
				"writes is in UTC, and a timeline that mixes the two reads as though things "+
				"happened out of order: %s",
			exposed.ExposedAt,
		)
	}
}
