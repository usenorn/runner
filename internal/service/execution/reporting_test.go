package execution_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func underWay(t *testing.T, h *harness, id string) {
	t.Helper()

	begun(t, h, id)

	h.await(t, "waited for the coding agent to be under way", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(channelv1.StateRunning) {
				return true
			}
		}

		return false
	})
}

func TestSayingWhereItIsUpToLandsOnTheTimelineWithThePhaseAndHowFar(t *testing.T) {
	h := newHarness(t, 2, 0)

	held := holds("session-01")
	h.drivers.scripts = []script{held}

	stop := h.start(t)
	defer stop()

	underWay(t, h, "exec-01ABC")

	err := h.service.Progress(context.Background(), "exec-01ABC", entity.Progress{
		Summary: "the migration is written and the tests are next",
		Phase:   "building",
		Percent: 60,
	})
	if err != nil {
		t.Fatalf("say where the run is up to: %v", err)
	}

	entry := h.awaitNote(t, "the migration is written")

	if entry.Kind != string(channelv1.EventPhase) {
		t.Fatalf(
			"progress arrived as a %s. Norn shows a phase where somebody following the issue "+
				"is looking, and a note somewhere else",
			entry.Kind,
		)
	}

	if !strings.HasPrefix(entry.Reason, "building: ") {
		t.Fatalf("the line reads %q and never says which phase it belongs to", entry.Reason)
	}

	var detail struct {
		Phase   string `json:"phase"`
		Percent int    `json:"percent"`
	}

	if err := json.Unmarshal(entry.Detail, &detail); err != nil {
		t.Fatalf("read what travelled with the progress line: %v", err)
	}

	if detail.Percent != 60 || detail.Phase != "building" {
		t.Fatalf(
			"what travelled with the line was %+v, so norn has prose and nothing it can draw",
			detail,
		)
	}

}

func TestProgressPastAHundredIsRefusedRatherThanShownAsIs(t *testing.T) {
	h := newHarness(t, 2, 0)

	held := holds("session-01")
	h.drivers.scripts = []script{held}

	stop := h.start(t)
	defer stop()

	underWay(t, h, "exec-01ABC")

	err := h.service.Progress(context.Background(), "exec-01ABC", entity.Progress{
		Summary: "nearly there",
		Percent: 140,
	})

	if !errors.Is(err, entity.ErrProgressRange) {
		t.Fatalf("a run reported itself 140%% done and nothing stopped it: %v", err)
	}
}

func finishing(t *testing.T, h *harness, id string, completion entity.Completion) script {
	t.Helper()

	held := holds("session-01")

	go func() {
		<-h.drivers.playing()

		if err := h.service.Complete(context.Background(), id, completion); err != nil {
			t.Error(err)
		}

		close(held.hold)
	}()

	return held
}

func TestARunFinishesOnWhatTheAgentSaidItDidRatherThanOnTheProcessExiting(t *testing.T) {
	h := newHarness(t, 2, 0)

	h.drivers.scripts = []script{finishing(t, h, "exec-01ABC", entity.Completion{
		Summary: "added a median helper and pinned it with a test",
		Notes:   "the even-length convention was decided by a person",
	})}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the run to reach finalizing", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(channelv1.StateFinalizing) {
				return strings.Contains(reported.Reason, "added a median helper")
			}
		}

		return false
	})

	for _, reported := range h.reports(t) {
		if reported.State != string(channelv1.StateFinalizing) {
			continue
		}

		if strings.Contains(reported.Reason, "held open by the test") {
			t.Fatalf(
				"the run finished on what the process reported rather than on what the agent "+
					"said it changed: %q. Nobody reading the issue wants a turn count",
				reported.Reason,
			)
		}

		if !strings.Contains(reported.Reason, "the even-length convention") {
			t.Fatalf("what the agent left for whoever reviews this was dropped: %q", reported.Reason)
		}
	}
}

func TestSayingTheWorkIsDoneWithNothingToSayIsRefused(t *testing.T) {
	h := newHarness(t, 2, 0)

	held := holds("session-01")
	h.drivers.scripts = []script{held}

	stop := h.start(t)
	defer stop()

	underWay(t, h, "exec-01ABC")

	err := h.service.Complete(context.Background(), "exec-01ABC", entity.Completion{})

	if !errors.Is(err, entity.ErrCompleteEmpty) {
		t.Fatalf(
			"a run was finished with no account of what changed: %v. That summary is what a "+
				"person reads before they open the diff",
			err,
		)
	}
}
