package execution_test

import (
	"context"
	"errors"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func (h *harness) reviewed(t *testing.T) func() {
	t.Helper()

	reviewable(h)
	h.opened = "https://github.com/usenorn/runner/pull/231"

	stop := h.start(t)

	begun(t, h, "exec-01ABC")
	h.awaitReview(t, "exec-01ABC")

	return stop
}

func (h *harness) reported(t *testing.T, wanted entity.ExecutionState) int {
	t.Helper()

	count := 0

	for _, report := range h.reports(t) {
		if report.State == string(wanted) {
			count++
		}
	}

	return count
}

func (h *harness) pullRequests() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.requested)
}

func TestAnApprovalOfHeadsThisRunDoesNotHoldPublishesNothingAndGoesBackToReview(t *testing.T) {
	h := newHarness(t, 2, 0)
	stop := h.reviewed(t)
	defer stop()

	approval := h.approval(t)
	approval.Heads = map[string]string{"runner": "a-commit-nobody-reviewed"}

	if err := h.service.Continue(context.Background(), "exec-01ABC", approval); err != nil {
		t.Fatalf("approve: %v", err)
	}

	h.await(t, "waited for the run to go back for review", func() bool {
		return h.reported(t, channelv1.StateAwaitingReview) == 2
	})

	if pushed := h.pushes(); len(pushed) != 0 {
		t.Fatalf("a stale approval pushed %v", pushed)
	}

	if results := h.sentOf(t, channelv1.ExecutionResult); len(results) != 2 ||
		decodeInto[channelv1.Result](t, results[1]).Revision != 2 {
		t.Fatalf("the refused approval did not put a fresh revision up for review: %d results", len(results))
	}
}

func TestACommitMadeAfterReviewTurnsTheApprovalIntoAFreshReview(t *testing.T) {
	h := newHarness(t, 2, 0)
	stop := h.reviewed(t)
	defer stop()

	approval := h.approval(t)

	h.mu.Lock()
	h.tip = "committed-by-a-preview-after-review"
	h.mu.Unlock()

	if err := h.service.Continue(context.Background(), "exec-01ABC", approval); err != nil {
		t.Fatalf("approve: %v", err)
	}

	h.await(t, "waited for the run to go back for review", func() bool {
		return h.reported(t, channelv1.StateAwaitingReview) == 2
	})

	if pushed := h.pushes(); len(pushed) != 0 {
		t.Fatalf("an approval of a branch that moved on pushed %v", pushed)
	}
}

func TestAPublicationThatStalledStaysApprovedUntilARetryFinishesIt(t *testing.T) {
	h := newHarness(t, 2, 0)
	stop := h.reviewed(t)
	defer stop()

	h.openErr = errors.New("HTTP 502")

	approval := h.approval(t)

	if err := h.service.Continue(context.Background(), "exec-01ABC", approval); err != nil {
		t.Fatalf("approve: %v", err)
	}

	h.awaitNote(t, entity.PublicationIncomplete([]entity.RepositoryPublication{{Repository: "runner"}}))

	if h.reported(t, channelv1.StateCompleted) != 0 {
		t.Fatal("a publication whose pull request never opened was reported as complete")
	}

	h.mu.Lock()
	h.openErr = nil
	h.mu.Unlock()

	approval.Reason = channelv1.ResumePublish

	if err := h.service.Continue(context.Background(), "exec-01ABC", approval); err != nil {
		t.Fatalf("retry: %v", err)
	}

	h.awaitState(t, "exec-01ABC", channelv1.StateWatching)

	if pushed := h.pushes(); len(pushed) != 2 {
		t.Fatalf("the branch was pushed %d times across the attempt and its retry", len(pushed))
	}

	if requests := h.pullRequests(); requests != 2 {
		t.Fatalf("%d pull requests were asked for across the attempt and its retry", requests)
	}
}

func TestGivingUpOnAStalledPublicationFailsTheRun(t *testing.T) {
	h := newHarness(t, 2, 0)
	stop := h.reviewed(t)
	defer stop()

	h.openErr = errors.New("HTTP 502")

	approval := h.approval(t)

	if err := h.service.Continue(context.Background(), "exec-01ABC", approval); err != nil {
		t.Fatalf("approve: %v", err)
	}

	h.awaitNote(t, entity.PublicationIncomplete([]entity.RepositoryPublication{{Repository: "runner"}}))

	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason: channelv1.ResumeAbandon,
	}); err != nil {
		t.Fatalf("give up: %v", err)
	}

	h.awaitState(t, "exec-01ABC", channelv1.StateFailed)
}

func TestTheSameApprovalDeliveredTwicePublishesOnce(t *testing.T) {
	h := newHarness(t, 2, 0)
	stop := h.reviewed(t)
	defer stop()

	approval := h.approval(t)

	for range 2 {
		if err := h.service.Continue(context.Background(), "exec-01ABC", approval); err != nil {
			t.Fatalf("approve: %v", err)
		}
	}

	h.awaitState(t, "exec-01ABC", channelv1.StateWatching)

	if requests := h.pullRequests(); requests != 1 {
		t.Fatalf("a duplicated approval asked for %d pull requests", requests)
	}
}
