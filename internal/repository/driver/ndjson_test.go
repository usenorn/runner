package driver_test

import (
	"strings"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func TestARecordedSessionComesBackAsMessagesToolCallsAndTheirResults(t *testing.T) {
	h := newHarness(t)

	events, logs, result := h.replay(t, "clean.ndjson")

	kinds := []entity.DriverEventKind{}

	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}

	want := []entity.DriverEventKind{
		entity.DriverEventMessage,
		entity.DriverEventToolCall,
		entity.DriverEventToolResult,
		entity.DriverEventMessage,
		entity.DriverEventUsage,
	}

	if len(kinds) != len(want) {
		t.Fatalf("a recorded session read back as %v", kinds)
	}

	for at, kind := range want {
		if kinds[at] != kind {
			t.Fatalf("a recorded session read back as %v", kinds)
		}
	}

	if events[1].Tool != "Read" {
		t.Fatalf("the tool the agent used came back as %q", events[1].Tool)
	}

	if events[2].Tool != "Read" {
		t.Fatalf("a tool result was not matched to the call that asked for it: %q", events[2].Tool)
	}

	if result.Outcome != entity.OutcomeDone || result.Summary != "hello from norn" {
		t.Fatalf("a session that finished came back as %+v", result)
	}

	if result.Usage.Turns != 2 || result.Usage.Took != 4846*time.Millisecond {
		t.Fatalf("what the session cost came back as %+v", result.Usage)
	}

	if len(logs) != 0 {
		t.Fatalf("a clean stream put %v where the logs go", logs)
	}
}

func TestEveryEventCarriesWhenItHappenedRatherThanWhenItWasRead(t *testing.T) {
	h := newHarness(t)

	events, _, _ := h.replay(t, "clean.ndjson")

	said, err := time.Parse(time.RFC3339, "2026-08-22T19:32:52.515Z")
	if err != nil {
		t.Fatalf("read the recorded time: %v", err)
	}

	if !events[0].At.Equal(said) {
		t.Fatalf("the first thing the agent said is stamped %s", events[0].At)
	}
}

func TestASessionThatWasRefusedItsToolsSaysHowManyThingsItCouldNotDo(t *testing.T) {
	h := newHarness(t)

	_, _, result := h.replay(t, "denied.ndjson")

	if result.Denials != 2 {
		t.Fatalf("a session refused two tools came back with %d denials", result.Denials)
	}

	if result.Outcome != entity.OutcomeDone {
		t.Fatalf("a session that was refused its tools still finished, and reads %+v", result)
	}
}

func TestASessionThatEndedInAnErrorIsNotReportedAsFinished(t *testing.T) {
	h := newHarness(t)

	_, _, result := h.replay(t, "failed.ndjson")

	if result.Outcome != entity.OutcomeFailed {
		t.Fatalf("a session that failed came back as %+v", result)
	}

	if result.Summary != "the model could not be reached" {
		t.Fatalf("a failed session explained itself as %q", result.Summary)
	}
}

func TestAStreamThatStopsPartWayThroughIsReportedAsACrashRatherThanASuccess(t *testing.T) {
	h := newHarness(t)

	events, _, result := h.replay(t, "truncated.ndjson")

	if result.Outcome != entity.OutcomeCrashed {
		t.Fatalf("a stream cut off mid-line came back as %+v", result)
	}

	if len(events) != 1 {
		t.Fatalf("a stream cut off mid-line gave up %d events", len(events))
	}
}

func TestALineThatIsNotAnEventGoesToTheLogsRatherThanTheFloor(t *testing.T) {
	h := newHarness(t)

	events, logs, result := h.replay(t, "noise.ndjson")

	if len(logs) != 1 || logs[0] != "npm warn: something the wrapper printed that is not json at all" {
		t.Fatalf("what the wrapper printed came back as %v", logs)
	}

	for _, event := range events {
		if event.Text == "kept out of the transcript" {
			t.Fatalf("the agent's thinking was put in the transcript")
		}
	}

	if result.Outcome != entity.OutcomeDone {
		t.Fatalf("a stream with noise in it came back as %+v", result)
	}
}

func TestTheSessionIdIsTakenFromTheStreamSoItCanBeCarriedOnLater(t *testing.T) {
	h := newHarness(t)

	h.replays(t, "clean.ndjson")

	session := h.start(t, entity.ProfileStandard)

	h.drain(t, session)

	if held := session.Reference(); held.ID != "dc39e0e7-426d-4cce-8b50-45351cfe2f49" {
		t.Fatalf("the session came back as %q, which is not what it called itself", held.ID)
	}
}

func TestALineLongerThanThisMachineKeepsIsDroppedRatherThanHeldWhole(t *testing.T) {
	h := newHarness(t)

	huge := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` +
		strings.Repeat("0123456789abcdef", entity.DriverLineMax/16+16) + "\"}]}}\n"

	t.Setenv("NORN_TEST_STREAM", write(t, h.dir, "huge.ndjson", huge))

	events, _, _ := h.drain(t, h.start(t, entity.ProfileStandard))

	if len(events) != 0 {
		t.Fatalf("a line past the cap still gave up %d events", len(events))
	}
}
