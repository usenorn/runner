package execution_test

import (
	"context"
	"os"
	"path/filepath"
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

func announcing(t *testing.T, h *harness, id, summary string) script {
	t.Helper()

	held := holds("session-01")

	go func() {
		<-h.drivers.playing()

		if err := h.service.Complete(
			context.Background(), id, entity.Completion{Summary: summary},
		); err != nil {
			t.Error(err)
		}

		close(held.hold)
	}()

	return held
}

func TestASecondPassReportsWhatItDidRatherThanWhatTheFirstPassSaid(t *testing.T) {
	h := newHarness(t, 2, 0)
	working(h)
	h.drivers.scripts = []script{
		announcing(t, h, "exec-01ABC", "added a median helper"),
		finishes("session-01", "added a mode helper"),
	}

	h.posts.EXPECT().
		PublishArtifact(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.ArtifactReceipt{ID: "f8b0a1c2-0000-4000-8000-000000000001"}, nil).
		AnyTimes()

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitReview(t, "exec-01ABC")

	first := decodeInto[channelv1.Result](t, h.sentOf(t, channelv1.ExecutionResult)[0])
	if first.Summary != "added a median helper" {
		t.Fatalf("the first pass reported %q", first.Summary)
	}

	if err := h.service.Continue(context.Background(), "exec-01ABC", channelv1.Instruction{
		Reason:      channelv1.ResumeFeedback,
		Instruction: "also add a mode helper",
	}); err != nil {
		t.Fatalf("ask for changes: %v", err)
	}

	h.await(t, "waited for the run to finish a second time", func() bool {
		return len(h.sentOf(t, channelv1.ExecutionResult)) >= 2
	})

	results := h.sentOf(t, channelv1.ExecutionResult)
	second := decodeInto[channelv1.Result](t, results[len(results)-1])

	if second.Summary == "added a median helper" {
		t.Fatal(
			"the amended result still describes the first pass; the coding agent is told to end " +
				"its turn without calling complete_task again, so a summary held over from " +
				"before the feedback is what a reviewer reads about work it never covers",
		)
	}

	if second.Summary != "added a mode helper" {
		t.Fatalf("the second pass reported %q", second.Summary)
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
		t.Fatalf(
			"the amended result still says %q; the agent is told to end its turn without calling "+
				"complete_task again, so a summary held over from before the feedback would "+
				"describe work the second pass did not do",
			second.Summary,
		)
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

func TestARunWaitingForReviewSurvivesTheMachineRestarting(t *testing.T) {
	h := newHarness(t, 2, 0)
	ctx := context.Background()

	fabricate(t, h, "exec-01ABC", channelv1.StateAwaitingReview)

	restarted := newHarnessOver(t, h, 2, 0)
	settled := restarted.start(t)

	defer settled()

	restarted.await(t, "waited for the run to be picked back up", func() bool {
		return len(restarted.service.Report(ctx).Executions) == 1
	})

	held := restarted.service.Report(ctx).Executions[0]

	if held.State != channelv1.StateAwaitingReview {
		t.Fatalf(
			"a run that was waiting for a person to review it came back as %s; its work is "+
				"pushed and its result is recorded, so throwing it away on a restart fails a run "+
				"that had already finished",
			held.State,
		)
	}

	if err := restarted.service.Reconcile(ctx, []string{"exec-01ABC"}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, reported := range restarted.reports(t) {
		if reported.State == string(channelv1.StateFailed) {
			t.Fatalf(
				"norn still expected the run and the machine had let go of it, so it was failed: "+
					"%q",
				reported.Reason,
			)
		}
	}

	if _, err := os.Stat(
		filepath.Join(h.dir.Run("exec-01ABC"), entity.RunWorkspaceDir),
	); err != nil {
		t.Fatalf(
			"the workspace was given back while the run was still in review: %v; asking for "+
				"changes carries on in the same folder on the same branches, and there would be "+
				"neither",
			err,
		)
	}
}
