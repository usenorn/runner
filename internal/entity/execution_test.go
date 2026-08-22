package entity_test

import (
	"path/filepath"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func TestOnlyWorkTheMachineIsActuallyDoingHoldsASlot(t *testing.T) {
	holding := map[entity.ExecutionState]bool{
		channelv1.StateQueued:          false,
		channelv1.StateLeased:          false,
		channelv1.StatePreparing:       true,
		channelv1.StateRunning:         true,
		channelv1.StateFinalizing:      true,
		channelv1.StateWaitingForInput: false,
		channelv1.StateAwaitingReview:  false,
		channelv1.StateQueuedForResume: false,
		channelv1.StateApproved:        false,
		channelv1.StateCompleted:       false,
		channelv1.StateFailed:          false,
		channelv1.StateCancelled:       false,
		channelv1.StateInterrupted:     false,
	}

	for state, want := range holding {
		execution := entity.Execution{State: state}

		if execution.HoldsSlot() != want {
			t.Errorf("%q holds a slot=%v, want %v", state, execution.HoldsSlot(), want)
		}
	}
}

func TestARunnerNeverReportsAStateNornWouldRefuse(t *testing.T) {
	cases := []struct {
		from     entity.ExecutionState
		to       entity.ExecutionState
		reported bool
	}{
		{from: channelv1.StateLeased, to: channelv1.StatePreparing, reported: true},
		{from: channelv1.StatePreparing, to: channelv1.StateRunning, reported: true},
		{from: channelv1.StateRunning, to: channelv1.StateFinalizing, reported: true},
		{from: channelv1.StateFinalizing, to: channelv1.StateAwaitingReview, reported: true},
		{from: channelv1.StateRunning, to: channelv1.StateFailed, reported: true},
		{from: channelv1.StateFinalizing, to: channelv1.StateCompleted, reported: false},
		{from: channelv1.StateRunning, to: channelv1.StateCancelled, reported: false},
		{from: channelv1.StateRunning, to: channelv1.StateInterrupted, reported: false},
		{from: channelv1.StateLeased, to: channelv1.StateRunning, reported: false},
		{from: channelv1.StatePreparing, to: channelv1.StateAwaitingReview, reported: false},
	}

	for _, want := range cases {
		execution := entity.Execution{State: want.from}

		if got := execution.CanReport(want.to); got != want.reported {
			t.Errorf(
				"reporting %q from %q was allowed=%v, want %v",
				want.to, want.from, got, want.reported,
			)
		}
	}
}

func TestAnOfferBecomesARunThatKnowsWhereItLives(t *testing.T) {
	accepted := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

	execution := entity.ExecutionOf(channelv1.Offer{
		ExecutionID: "exec-01ABC",
		Reference:   "NORN-45-r2",
		Attempt:     2,
		WorkspaceID: "01WORKSPACE",
		Issue: channelv1.Issue{
			Reference:   "NORN-45",
			Title:       "Channel client",
			Description: "the daemon's half of the control plane",
			Brief:       "start with the spool",
		},
		Params: channelv1.Params{Tool: "claude-code", Model: "opus", Runtime: "process"},
	}, "/state/runs", accepted)

	if execution.Directory != filepath.Join("/state/runs", "exec-01ABC") {
		t.Fatalf("the run lives at %q", execution.Directory)
	}

	if execution.Metadata() != filepath.Join(execution.Directory, entity.RunMetadataDir) {
		t.Fatalf("the run keeps its metadata at %q", execution.Metadata())
	}

	if execution.State != channelv1.StateLeased {
		t.Fatalf("a run that has only been accepted is already %q", execution.State)
	}

	if execution.IssueKey != "NORN-45" || execution.Reference != "NORN-45-r2" {
		t.Fatalf("the run names issue %q as %q", execution.IssueKey, execution.Reference)
	}

	if execution.Attempt != 2 || execution.AcceptedAt != accepted {
		t.Fatalf("the run is attempt %d, accepted at %s", execution.Attempt, execution.AcceptedAt)
	}
}

func TestAnOfferThatCountsNoAttemptIsStillTheFirstOne(t *testing.T) {
	execution := entity.ExecutionOf(channelv1.Offer{ExecutionID: "exec-01ABC"}, "/runs", time.Now())

	if execution.Attempt != 1 {
		t.Fatalf("an uncounted offer became attempt %d", execution.Attempt)
	}
}
