package execution_test

import (
	"context"
	"errors"
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

func TestAFinishedRunTellsNornWhatItChangedAndPushesOnlyOnceApproved(t *testing.T) {
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

	if pushed := h.pushes(); len(pushed) != 0 {
		t.Fatalf(
			"a run waiting for review pushed %v; nothing leaves this machine until a person has "+
				"reviewed the changes in norn and approved them",
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

	approved(t, h, "exec-01ABC")
	h.awaitState(t, "exec-01ABC", channelv1.StateCompleted)

	if pushed := h.pushes(); len(pushed) != 1 || !strings.Contains(pushed[0], "NORN-47") {
		t.Fatalf(
			"an approved run pushed %v; approving is what publishes the branch",
			pushed,
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

	if first.Revision != 1 || second.Revision != 2 {
		t.Fatalf(
			"the passes were numbered %d and %d; each pass is a new review snapshot, and "+
				"comments are tied to the one they were left on",
			first.Revision, second.Revision,
		)
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

	if pushed := h.pushes(); len(pushed) != 0 {
		t.Fatalf(
			"asking for changes pushed %v; the second pass goes back to review in norn first",
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

func TestARunApprovedJustBeforeTheMachineStoppedIsCompletedAfterItRestarts(t *testing.T) {
	h := newHarness(t, 2, 0)

	fabricate(t, h, "exec-01ABC", channelv1.StateApproved)

	ctx := context.Background()
	if err := h.runs.SaveReview(ctx, "exec-01ABC", entity.Review{Revision: 1}); err != nil {
		t.Fatalf("write the review by hand: %v", err)
	}

	if err := h.runs.SaveApproval(ctx, "exec-01ABC", channelv1.Instruction{Revision: 1}); err != nil {
		t.Fatalf("write the approval by hand: %v", err)
	}

	restarted := newHarnessOver(t, h, 2, 0)
	settled := restarted.start(t)

	defer settled()

	restarted.awaitState(t, "exec-01ABC", channelv1.StateCompleted)

	if kept := restarted.service.Report(context.Background()).Executions; len(kept) != 0 {
		t.Fatalf("a run somebody approved is still held after the restart: %+v", kept)
	}
}

func TestARunMarkedApprovedWithNoApprovalWrittenDownIsReviewedAgainRatherThanPublished(t *testing.T) {
	h := newHarness(t, 2, 0)

	fabricate(t, h, "exec-01ABC", channelv1.StateApproved)

	restarted := newHarnessOver(t, h, 2, 0)
	settled := restarted.start(t)

	defer settled()

	restarted.await(t, "waited for the run to be taken back from publishing", func() bool {
		for _, reported := range restarted.reports(t) {
			if reported.State != string(channelv1.StateApproved) {
				return true
			}
		}

		return false
	})

	for _, reported := range restarted.reports(t) {
		if reported.State == string(channelv1.StateCompleted) {
			t.Fatalf("a run nobody is known to have approved was completed after the restart: %+v", reported)
		}
	}

	if pushed := restarted.pushes(); len(pushed) != 0 {
		t.Fatalf("a run nobody is known to have approved was published after the restart: %v", pushed)
	}
}

func (h *harness) pushes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]string(nil), h.pushed...)
}

func TestEveryPreviewThePlanDeclaresIsPreparedBeforeReviewOrSaysWhyNot(t *testing.T) {
	h := newHarness(t, 2, 0)
	working(h)
	h.planFile = "/codebase/.norn/run-plan.yaml"
	h.plan = entity.PlanDefinition{
		Services: []entity.PlanService{
			{Kind: entity.PlanServiceCompose, Service: entity.Service{Name: "postgres"}},
			{Kind: entity.PlanServiceProcess, Service: entity.Service{Name: "api", Command: []string{"api"}}},
			{Kind: entity.PlanServiceProcess, Service: entity.Service{
				Name: "web", Command: []string{"web"}, Requires: []string{"api"},
			}},
			{Kind: entity.PlanServiceProcess, Service: entity.Service{Name: "docs", Command: []string{"docs"}}},
		},
		Previews: []entity.PlanPreview{
			{Name: "Documentation", Service: "docs"},
			{Name: "Application", Service: "web"},
			{Name: "API", Service: "api"},
			{Name: "Database", Service: "postgres"},
			{Name: "Admin", Service: "admin"},
		},
	}
	h.failing = map[string]string{"api": "it stopped on its own with exit code 1"}
	h.drivers.scripts = []script{finishes("session-01", "added a median helper")}

	h.posts.EXPECT().
		PublishArtifact(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.ArtifactReceipt{ID: "f8b0a1c2-0000-4000-8000-000000000001"}, nil).
		AnyTimes()

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitReview(t, "exec-01ABC")

	result := decodeInto[channelv1.Result](t, h.only(t, channelv1.ExecutionResult))

	states := map[string]string{}
	for _, preview := range result.Previews {
		states[preview.Name] = preview.State + ": " + preview.Reason
	}

	for name, want := range map[string]string{
		"Documentation": "ready",
		"Application":   "failed: it needs api",
		"API":           "failed: it stopped on its own",
		"Database":      "unsupported: postgres is a compose service",
		"Admin":         "unsupported: the run plan declares no service named admin",
	} {
		if !strings.HasPrefix(states[name], want) {
			t.Errorf("%s was reported as %q, want it to start %q", name, states[name], want)
		}
	}

	if len(result.Previews) != len(h.plan.Previews) {
		t.Fatalf("%d of %d previews were reported; none may go missing", len(result.Previews), len(h.plan.Previews))
	}

	if strings.Join(h.started, ",") != "api,docs" {
		t.Fatalf("started %v; web needs api, which never came up", h.started)
	}
}

func TestTheAgentAnswersOnlyTheThreadsTheReviewSentBack(t *testing.T) {
	h := newHarness(t, 2, 0)
	working(h)
	h.drivers.scripts = []script{
		finishes("session-01", "added a median helper"),
		finishes("session-01", "returned the error"),
	}

	h.posts.EXPECT().
		PublishArtifact(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(entity.ArtifactReceipt{ID: "f8b0a1c2-0000-4000-8000-000000000001"}, nil).
		AnyTimes()

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitReview(t, "exec-01ABC")

	ctx := context.Background()
	thread := "5b0c7a3e-2d1f-4e7a-9c1b-0f6d2a8e4b11"

	if err := h.service.Reply(ctx, "exec-01ABC", entity.ReviewReply{CommentID: thread, Body: "early"}); !errors.Is(
		err, entity.ErrReplyUnasked,
	) {
		t.Fatalf("answering before any feedback arrived answered %v", err)
	}

	if err := h.service.Continue(ctx, "exec-01ABC", channelv1.Instruction{
		Reason:      channelv1.ResumeFeedback,
		Instruction: "This swallows the error.",
		Threads:     []string{thread},
	}); err != nil {
		t.Fatalf("ask for changes: %v", err)
	}

	h.await(t, "waited for the run to finish a second time", func() bool {
		return len(h.sentOf(t, channelv1.ExecutionResult)) >= 2
	})

	if err := h.service.Reply(ctx, "exec-01ABC", entity.ReviewReply{
		CommentID: thread, Body: "  Returned the error instead.  ",
	}); err != nil {
		t.Fatalf("answer the thread: %v", err)
	}

	reply := decodeInto[channelv1.ReviewReply](t, h.only(t, channelv1.ReviewReplied))

	if reply.CommentID != thread || reply.Body != "Returned the error instead." {
		t.Fatalf("norn was told %+v", reply)
	}

	if err := h.service.Reply(ctx, "exec-01ABC", entity.ReviewReply{
		CommentID: "00000000-0000-4000-8000-000000000000", Body: "Done.",
	}); !errors.Is(err, entity.ErrReplyUnasked) {
		t.Fatalf("answering a thread the review never sent answered %v", err)
	}

	if err := h.service.Reply(ctx, "exec-01ABC", entity.ReviewReply{CommentID: thread, Body: "  "}); !errors.Is(
		err, entity.ErrReplyEmpty,
	) {
		t.Fatalf("an empty answer answered %v", err)
	}
}
