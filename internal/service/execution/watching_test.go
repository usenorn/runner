package execution_test

import (
	"context"
	"strings"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

const watchedPullRequest = "https://github.com/usenorn/runner/pull/231"

func (h *harness) pull(status entity.PullRequestStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()

	status.URL = watchedPullRequest

	h.pulls = map[string]entity.PullRequestStatus{watchedPullRequest: status}
}

func (h *harness) watching(t *testing.T) func() {
	t.Helper()

	stop := h.reviewed(t)

	if err := h.service.Continue(context.Background(), "exec-01ABC", h.approval(t)); err != nil {
		t.Fatalf("approve: %v", err)
	}

	h.awaitState(t, "exec-01ABC", channelv1.StateWatching)

	return stop
}

func (h *harness) then(next script) {
	h.drivers.mu.Lock()
	defer h.drivers.mu.Unlock()

	h.drivers.scripts = append(h.drivers.scripts, next)
}

func (h *harness) reason(t *testing.T, state entity.ExecutionState) string {
	t.Helper()

	said := ""

	for _, report := range h.reports(t) {
		if report.State == string(state) {
			said = report.Reason
		}
	}

	return said
}

func TestAMergedPullRequestFinishesTheRun(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.pull(entity.PullRequestStatus{State: entity.PullRequestOpen, Head: "head-sha"})

	stop := h.watching(t)
	defer stop()

	h.pull(entity.PullRequestStatus{State: entity.PullRequestMerged, Head: "head-sha"})
	h.awaitState(t, "exec-01ABC", channelv1.StateCompleted)

	if said := h.reason(t, channelv1.StateCompleted); !strings.Contains(said, "merged") {
		t.Fatalf("the run finished saying %q, which does not say the pull request was merged", said)
	}
}

func TestAPullRequestClosedWithoutMergingStopsTheRun(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.pull(entity.PullRequestStatus{State: entity.PullRequestOpen, Head: "head-sha"})

	stop := h.watching(t)
	defer stop()

	h.pull(entity.PullRequestStatus{State: entity.PullRequestClosed, Head: "head-sha"})
	h.awaitState(t, "exec-01ABC", channelv1.StateFailed)
}

func TestANewReviewCommentBringsTheRunBackAndItsAnswerIsPostedOnceApproved(t *testing.T) {
	h := newHarness(t, 2, 0)

	earlier := entity.PullRequestComment{
		Kind: entity.CommentConversation, ID: "100", Author: "rae", Body: "thanks for picking this up",
	}
	h.pull(entity.PullRequestStatus{
		State: entity.PullRequestOpen, Head: "head-sha", Comments: []entity.PullRequestComment{earlier},
	})

	stop := h.watching(t)
	defer stop()

	h.then(finishes("session-01", "renamed the helper"))

	asked := entity.PullRequestComment{
		Kind: entity.CommentInline, ID: "200", Author: "rae", Body: "call it median, not mid",
		Path: "src/stats.go", Line: 12,
	}
	h.pull(entity.PullRequestStatus{
		State: entity.PullRequestOpen, Head: "head-sha", Comments: []entity.PullRequestComment{earlier, asked},
	})

	h.await(t, "waited for the review comment to bring the run back", func() bool {
		return len(h.drivers.injections()) > 0
	})

	told := h.drivers.injections()[0]

	if !strings.Contains(told, "call it median, not mid") || !strings.Contains(told, "src/stats.go line 12") {
		t.Fatalf("the agent was told %q rather than the review comment", told)
	}

	if strings.Contains(told, "thanks for picking this up") {
		t.Fatalf(
			"a comment that was on the pull request before the run began watching it was handed "+
				"to the agent as new:\n%s",
			told,
		)
	}

	h.await(t, "waited for the second pass to come back for review", func() bool {
		return h.reported(t, channelv1.StateAwaitingReview) == 2 || h.reported(t, channelv1.StateFailed) > 0
	})

	if failed := h.reason(t, channelv1.StateFailed); failed != "" {
		t.Fatalf("the second pass failed: %s", failed)
	}

	thread := asked.Thread(watchedPullRequest)

	if err := h.service.Reply(context.Background(), "exec-01ABC", entity.ReviewReply{
		CommentID: thread, Body: "renamed it to median",
	}); err != nil {
		t.Fatalf("answer the pull request comment: %v", err)
	}

	if err := h.service.Continue(context.Background(), "exec-01ABC", h.approval(t)); err != nil {
		t.Fatalf("approve the second revision: %v", err)
	}

	h.await(t, "waited for the answer to reach the pull request", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()

		return len(h.posted) == 1
	})

	h.mu.Lock()
	posted := h.posted[0]
	h.mu.Unlock()

	if posted != "200: renamed it to median" {
		t.Fatalf("the pull request was answered with %q", posted)
	}

	h.awaitState(t, "exec-01ABC", channelv1.StateWatching)
}

func TestAFailedCheckBringsTheRunBackWithTheEndOfItsLog(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.pull(entity.PullRequestStatus{State: entity.PullRequestOpen, Head: "head-sha"})

	stop := h.watching(t)
	defer stop()

	h.then(finishes("session-01", "fixed the median"))

	h.pull(entity.PullRequestStatus{
		State: entity.PullRequestOpen, Head: "head-sha",
		Failed: []entity.FailedCheck{{Name: "test", URL: "https://github.com/usenorn/runner/actions/runs/1/job/2"}},
	})

	h.await(t, "waited for the failed check to bring the run back", func() bool {
		return len(h.drivers.injections()) > 0
	})

	if told := h.drivers.injections()[0]; !strings.Contains(told, "expected 2.5, got 2") {
		t.Fatalf("the agent was told %q without the failing log", told)
	}
}
