package execution_test

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func working(h *harness) {
	h.commits = 2
	h.stat = entity.Diffstat{Additions: 40, Deletions: 3, Files: 4}
}

func TestAFinishedRunPushesItsBranchAndTellsNornWhatItChanged(t *testing.T) {
	h := newHarness(t, 2, 0)
	working(h)
	h.drivers.scripts = []script{finishes("session-01", "added a median helper")}

	h.posts.EXPECT().
		PublishArtifact(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.ArtifactReceipt{ID: "f8b0a1c2-0000-4000-8000-000000000001"}, nil).
		AnyTimes()

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitReview(t, "exec-01ABC")

	h.mu.Lock()
	pushed := append([]string(nil), h.pushed...)
	h.mu.Unlock()

	if len(pushed) != 1 || !strings.Contains(pushed[0], "NORN-47") {
		t.Fatalf(
			"a finished run pushed %v; without a push the work never leaves this machine and "+
				"nobody can review it",
			pushed,
		)
	}

	changes := h.only(t, channelv1.ChangeSetUpdated)
	reported := decodeInto[channelv1.ChangeSet](t, changes)

	if len(reported.Repos) != 1 || reported.Repos[0].Commits != 2 {
		t.Fatalf("norn was told %+v", reported.Repos)
	}

	result := decodeInto[channelv1.Result](t, h.only(t, channelv1.ExecutionResult))

	if result.Summary != "added a median helper" {
		t.Fatalf(
			"the run's result says %q; that summary is what a person reads first on the review "+
				"screen",
			result.Summary,
		)
	}
}

func TestAnAgentThatLeftWorkUncommittedIsAskedToCommitItRatherThanHavingItDoneForIt(t *testing.T) {
	h := newHarness(t, 2, 0)
	working(h)
	h.dirty = map[string][]string{"runner": {"src/median.go"}}
	h.drivers.scripts = []script{
		finishes("session-01", "done"),
		finishes("session-01", "committed it now"),
	}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.await(t, "waited for the agent to be asked to commit", func() bool {
		return len(h.drivers.injections()) > 0
	})

	asked := h.drivers.injections()[0]

	if !strings.Contains(asked, "src/median.go") {
		t.Fatalf(
			"the agent was told %q, which does not name the file it left behind, so it has to "+
				"guess what to commit",
			asked,
		)
	}

	h.mu.Lock()
	pushed := len(h.pushed)
	h.mu.Unlock()

	if pushed != 0 {
		t.Fatal(
			"a run with uncommitted work pushed anyway, so the branch is missing changes the " +
				"diff and the pull request both claim to cover",
		)
	}
}

func TestAnAgentThatLeavesWorkUncommittedTwiceFailsRatherThanReachingReview(t *testing.T) {
	h := newHarness(t, 2, 0)
	working(h)
	h.dirty = map[string][]string{"runner": {"src/median.go"}}
	h.drivers.scripts = []script{
		finishes("session-01", "done"),
		finishes("session-01", "done again"),
	}

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitState(t, "exec-01ABC", channelv1.StateFailed)

	for _, reported := range h.reports(t) {
		if reported.State == string(channelv1.StateAwaitingReview) {
			t.Fatal(
				"a run that never committed its work reached review, so a person would open a " +
					"branch that is missing the change they were asked to look at",
			)
		}
	}

	failed := ""

	for _, reported := range h.reports(t) {
		if reported.State == string(channelv1.StateFailed) {
			failed = reported.Reason
		}
	}

	if !strings.Contains(failed, "uncommitted") {
		t.Fatalf("the run failed saying %q, which does not say why", failed)
	}
}

func TestARunAskedForChangesCarriesOnRatherThanBeingDropped(t *testing.T) {
	h := newHarness(t, 2, 0)
	working(h)
	h.drivers.scripts = []script{
		finishes("session-01", "first pass"),
		finishes("session-01", "took the feedback"),
	}

	h.posts.EXPECT().
		PublishArtifact(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.ArtifactReceipt{ID: "f8b0a1c2-0000-4000-8000-000000000001"}, nil).
		AnyTimes()

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitReview(t, "exec-01ABC")

	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason:      channelv1.ResumeFeedback,
		Instruction: "please rename the helper",
	}); err != nil {
		t.Fatalf("ask for changes: %v", err)
	}

	h.await(t, "waited for the run to carry on", func() bool {
		return len(h.drivers.injections()) > 0
	})

	if asked := h.drivers.injections()[0]; !strings.Contains(asked, "rename the helper") {
		t.Fatalf("the agent was told %q rather than the review feedback", asked)
	}

	h.await(t, "waited for the run to finish a second time", func() bool {
		return len(h.sentOf(t, channelv1.ExecutionResult)) >= 2
	})

	results := h.sentOf(t, channelv1.ExecutionResult)
	first := decodeInto[channelv1.Result](t, results[0])
	second := decodeInto[channelv1.Result](t, results[len(results)-1])

	if !second.Reported.After(first.Reported) {
		t.Fatalf(
			"the amended result is stamped %s and the first %s; norn keeps whichever is newer, "+
				"so a result that does not move forward is silently thrown away",
			second.Reported, first.Reported,
		)
	}

	if second.Summary != "took the feedback" {
		t.Fatalf("the amended result still says %q", second.Summary)
	}

	h.mu.Lock()
	pushed := append([]string(nil), h.pushed...)
	h.mu.Unlock()

	if len(pushed) != 2 || pushed[0] != pushed[1] {
		t.Fatalf(
			"the second pass pushed %v; asking for changes has to add commits to the branch the "+
				"first pass opened, not start a second one",
			pushed,
		)
	}
}
