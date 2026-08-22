package spool_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/repository/spool"
)

func newSpool(t *testing.T) (repository.Spool, *statedir.Dir) {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("make a state directory: %v", err)
	}

	return spool.New(dir), dir
}

func message(t *testing.T, kind channelv1.MessageType, issued time.Time) channelv1.Message {
	t.Helper()

	held, err := channelv1.NewRunnerMessage(kind, "exec-01ABC", []byte(`{"state":"running"}`), issued)
	if err != nil {
		t.Fatalf("build a %s: %v", kind, err)
	}

	return held
}

func TestWhatIsSpooledComesBackInTheOrderItWasWrittenEvenAfterARestart(t *testing.T) {
	held, dir := newSpool(t)
	ctx := context.Background()

	written := make([]string, 0, 5)

	for range 5 {
		queued := message(t, channelv1.ExecutionEvent, time.Now().UTC())

		if err := held.Append(ctx, queued); err != nil {
			t.Fatalf("append: %v", err)
		}

		written = append(written, queued.ID)
	}

	restarted := spool.New(dir)

	waiting, err := restarted.Head(ctx, 0)
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	if len(waiting) != len(written) {
		t.Fatalf("the spool held %d messages after a restart, want %d", len(waiting), len(written))
	}

	for index, queued := range waiting {
		if queued.ID != written[index] {
			t.Fatalf(
				"the spool gave back %s at position %d, want %s", queued.ID, index, written[index],
			)
		}

		if queued.Type != channelv1.ExecutionEvent || queued.ExecutionID != "exec-01ABC" {
			t.Fatalf("the spool changed a message into %+v", queued)
		}

		if string(queued.Payload) != `{"state":"running"}` {
			t.Fatalf("the spool gave back the payload %q", queued.Payload)
		}
	}
}

func TestHeadGivesBackNoMoreThanItIsAskedFor(t *testing.T) {
	held, _ := newSpool(t)
	ctx := context.Background()

	for range 4 {
		if err := held.Append(ctx, message(t, channelv1.ExecutionEvent, time.Now().UTC())); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	waiting, err := held.Head(ctx, 2)
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	if len(waiting) != 2 {
		t.Fatalf("a batch of 2 gave back %d messages", len(waiting))
	}
}

func TestAcknowledgingAMessageTakesThatOneAndNoOtherOutOfTheSpool(t *testing.T) {
	held, _ := newSpool(t)
	ctx := context.Background()

	first := message(t, channelv1.ExecutionEvent, time.Now().UTC())
	second := message(t, channelv1.ExecutionEvent, time.Now().UTC())

	for _, queued := range []channelv1.Message{first, second} {
		if err := held.Append(ctx, queued); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	if err := held.Acknowledge(ctx, first.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	waiting, err := held.Head(ctx, 0)
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	if len(waiting) != 1 || waiting[0].ID != second.ID {
		t.Fatalf("the spool holds %+v, want only %s", waiting, second.ID)
	}

	if err := held.Acknowledge(ctx, first.ID); err != nil {
		t.Fatalf("acknowledging the same message twice: %v", err)
	}
}

func TestEventsThatWaitedTooLongForNornAreDropped(t *testing.T) {
	held, _ := newSpool(t)
	ctx := context.Background()

	stale := message(t, channelv1.ExecutionEvent, time.Now().UTC().Add(-48*time.Hour))
	fresh := message(t, channelv1.ExecutionEvent, time.Now().UTC())

	for _, queued := range []channelv1.Message{stale, fresh} {
		if err := held.Append(ctx, queued); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	dropped, err := held.Prune(ctx, time.Now().UTC().Add(-24*time.Hour), 0)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if dropped != 1 {
		t.Fatalf("pruning dropped %d messages, want the one that waited too long", dropped)
	}

	waiting, err := held.Head(ctx, 0)
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	if len(waiting) != 1 || waiting[0].ID != fresh.ID {
		t.Fatalf("the spool holds %+v after pruning", waiting)
	}
}

func TestASpoolThatGrowsPastItsCeilingLosesItsOldestEventsFirst(t *testing.T) {
	held, _ := newSpool(t)
	ctx := context.Background()

	written := make([]string, 0, 5)

	for range 5 {
		queued := message(t, channelv1.ExecutionEvent, time.Now().UTC())

		if err := held.Append(ctx, queued); err != nil {
			t.Fatalf("append: %v", err)
		}

		written = append(written, queued.ID)
	}

	dropped, err := held.Prune(ctx, time.Now().UTC().Add(-time.Hour), 2)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if dropped != 3 {
		t.Fatalf("keeping 2 of 5 dropped %d", dropped)
	}

	waiting, err := held.Head(ctx, 0)
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	if len(waiting) != 2 || waiting[0].ID != written[3] || waiting[1].ID != written[4] {
		t.Fatalf("the spool kept %+v, want the two newest", waiting)
	}
}

func TestSomethingThatIsNotASpooledMessageIsIgnoredRatherThanRead(t *testing.T) {
	held, dir := newSpool(t)
	ctx := context.Background()

	queued := message(t, channelv1.ExecutionEvent, time.Now().UTC())

	if err := held.Append(ctx, queued); err != nil {
		t.Fatalf("append: %v", err)
	}

	for _, name := range []string{"01AAAA.json", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir.Spool(), name), []byte("half a f"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	waiting, err := held.Head(ctx, 0)
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	if len(waiting) != 1 || waiting[0].ID != queued.ID {
		t.Fatalf("the spool gave back %+v, want only the message it wrote", waiting)
	}

	count, err := held.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	if count != 2 {
		t.Fatalf("the spool counted %d files, want the two that end in .json", count)
	}
}

func TestAnEmptySpoolIsNotAFailure(t *testing.T) {
	held, _ := newSpool(t)
	ctx := context.Background()

	waiting, err := held.Head(ctx, 0)
	if err != nil || len(waiting) != 0 {
		t.Fatalf("an empty spool gave back %+v, %v", waiting, err)
	}

	count, err := held.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("an empty spool counted %d, %v", count, err)
	}
}
