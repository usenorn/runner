package upload_test

import (
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestABatchIsSentOnceEnoughOfTheStreamHasBuiltUp(t *testing.T) {
	h := newHarness(t, settings())
	ctx := t.Context()

	if _, err := h.service.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open: %v", err)
	}

	h.service.Event(ctx, "exec-01ABC", said("one"))

	if len(h.sent()) != 0 {
		t.Fatalf("a single event was sent on its own")
	}

	h.service.Event(ctx, "exec-01ABC", said("two"))

	sent := h.sent()

	if len(sent) != 1 || len(sent[0].Entries) != 2 {
		t.Fatalf("what went to norn reads %+v", sent)
	}

	if sent[0].Sequence != 1 {
		t.Fatalf("the first batch of a stream sat at position %d", sent[0].Sequence)
	}
}

func TestAStreamCarriesOnFromWhereNornSaysItGotTo(t *testing.T) {
	h := newHarness(t, settings())
	ctx := t.Context()

	h.cursors = []entity.StreamCursor{
		{Stream: entity.StreamTranscript, LastSequence: 7},
		{Stream: entity.StreamLogs, LastSequence: 3},
	}

	if _, err := h.service.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open: %v", err)
	}

	h.service.Event(ctx, "exec-01ABC", said("one"))
	h.service.Event(ctx, "exec-01ABC", said("two"))
	h.service.Line(ctx, "exec-01ABC", entity.LogLine{Text: "a"})
	h.service.Line(ctx, "exec-01ABC", entity.LogLine{Text: "b"})

	if sent := h.sent(); len(sent) != 1 || sent[0].Sequence != 8 {
		t.Fatalf("a transcript norn already had seven of carried on at %+v", sent)
	}

	if said := h.said(); len(said) != 1 || said[0].Sequence != 4 {
		t.Fatalf("logs norn already had three of carried on at %+v", said)
	}
}

func TestMoreThanNornStoresInOneBatchIsSplitRatherThanRefused(t *testing.T) {
	cfg := settings()
	cfg.Batch = 4
	cfg.MaxChunkBytes = 64

	h := newHarness(t, cfg)
	ctx := t.Context()

	if _, err := h.service.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open: %v", err)
	}

	for range 4 {
		h.service.Event(ctx, "exec-01ABC", said(strings.Repeat("x", 40)))
	}

	sent := h.sent()

	if len(sent) < 2 {
		t.Fatalf("four events that will not fit in one batch went as %d batch(es)", len(sent))
	}

	for at, batch := range sent {
		if batch.Sequence != int64(at+1) {
			t.Fatalf("the batches went out as %+v, which leaves a hole in the stream", sent)
		}
	}
}

func TestAWorkspaceKeepingSummariesOnlySendsNoTranscriptAndStillSendsItsLogs(t *testing.T) {
	h := newHarness(t, settings())
	ctx := t.Context()

	h.mode = entity.TelemetryMinimal

	mode, err := h.service.Open(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if mode != entity.TelemetryMinimal {
		t.Fatalf("a workspace keeping summaries only was read as %q", mode)
	}

	h.service.Event(ctx, "exec-01ABC", used("Read"))
	h.service.Event(ctx, "exec-01ABC", said("a long answer"))

	if sent := h.sent(); len(sent) != 0 {
		t.Fatalf("a workspace keeping summaries only was sent a transcript: %+v", sent)
	}

	said := h.said()

	if len(said) != 1 || len(said[0].Entries) != 2 {
		t.Fatalf("what a summary-only workspace was sent reads %+v", said)
	}

	if !strings.Contains(said[0].Entries[0].Text, "Read") {
		t.Fatalf("the summary of a tool call reads %q", said[0].Entries[0].Text)
	}

	for _, line := range said[0].Entries {
		if strings.Contains(line.Text, "a long answer") {
			t.Fatalf("a summary carried what the agent actually said: %q", line.Text)
		}
	}
}

func TestABatchNornWillNeverTakeIsDroppedSoTheOnesBehindItStillGoOut(t *testing.T) {
	h := newHarness(t, settings())
	ctx := t.Context()

	if _, err := h.service.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open: %v", err)
	}

	h.refusing(entity.ErrUploadPositionTaken)

	h.service.Event(ctx, "exec-01ABC", said("one"))
	h.service.Event(ctx, "exec-01ABC", said("two"))

	h.refusing(nil)

	h.service.Event(ctx, "exec-01ABC", said("three"))
	h.service.Event(ctx, "exec-01ABC", said("four"))

	sent := h.sent()

	if len(sent) != 1 || sent[0].Entries[0].Text != "three" {
		t.Fatalf("a batch norn refused stalled the ones behind it: %+v", sent)
	}
}

func TestAServerOutOfReachHoldsTheStreamBackAndSendsItInOrderWhenItReturns(t *testing.T) {
	h := newHarness(t, settings())
	ctx := t.Context()

	if _, err := h.service.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open: %v", err)
	}

	h.refusing(entity.ErrServerUnreachable)

	h.service.Event(ctx, "exec-01ABC", said("one"))
	h.service.Event(ctx, "exec-01ABC", said("two"))
	h.service.Event(ctx, "exec-01ABC", said("three"))
	h.service.Event(ctx, "exec-01ABC", said("four"))

	if sent := h.sent(); len(sent) != 0 {
		t.Fatalf("something reached a server that was out of reach: %+v", sent)
	}

	h.refusing(nil)

	if err := h.service.Flush(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("flush once norn is back: %v", err)
	}

	sent := h.sent()

	if len(sent) != 2 {
		t.Fatalf("what was held back went out as %+v", sent)
	}

	if sent[0].Sequence != 1 || sent[1].Sequence != 2 {
		t.Fatalf("what was held back went out in the order %+v", sent)
	}
}

func TestAServerOutOfReachForLongEnoughLosesTheOldestRatherThanTheNewest(t *testing.T) {
	cfg := settings()
	cfg.MaxPending = 2

	h := newHarness(t, cfg)
	ctx := t.Context()

	if _, err := h.service.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open: %v", err)
	}

	h.refusing(entity.ErrServerUnreachable)

	for range 10 {
		h.service.Event(ctx, "exec-01ABC", said("one"))
		h.service.Event(ctx, "exec-01ABC", said("two"))
	}

	h.refusing(nil)

	if err := h.service.Flush(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("flush once norn is back: %v", err)
	}

	sent := h.sent()

	if len(sent) != 2 {
		t.Fatalf("a long outage sent %d batches, and it should keep only two", len(sent))
	}

	if sent[1].Sequence != 10 {
		t.Fatalf("a long outage kept batches ending at %d rather than the newest", sent[1].Sequence)
	}
}

func TestClosingARunSendsWhatIsLeftAndThenForgetsIt(t *testing.T) {
	h := newHarness(t, settings())
	ctx := t.Context()

	if _, err := h.service.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open: %v", err)
	}

	h.service.Event(ctx, "exec-01ABC", said("the only thing it said"))
	h.service.Close(ctx, "exec-01ABC")

	sent := h.sent()

	if len(sent) != 1 || len(sent[0].Entries) != 1 {
		t.Fatalf("what was left when the run closed reads %+v", sent)
	}

	h.service.Event(ctx, "exec-01ABC", said("said after the run was over"))

	if len(h.sent()) != 1 {
		t.Fatalf("a run that is over was still taking events")
	}
}

func TestARunNothingOpenedIsIgnoredRatherThanInvented(t *testing.T) {
	h := newHarness(t, settings())
	ctx := t.Context()

	h.service.Event(ctx, "exec-NEVER", said("one"))
	h.service.Line(ctx, "exec-NEVER", entity.LogLine{Text: "a"})

	if err := h.service.Flush(ctx, "exec-NEVER"); err != nil {
		t.Fatalf("flush a run nothing opened: %v", err)
	}

	if len(h.sent()) != 0 || len(h.said()) != 0 {
		t.Fatalf("a run nothing opened had something sent for it")
	}
}
