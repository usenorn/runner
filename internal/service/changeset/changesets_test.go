package changeset_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
)

func TestWhatEachRepositoryChangedIsReportedWithNumbersAReviewerCanTotal(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(3, entity.Diffstat{Additions: 412, Deletions: 77, Files: 9})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")
	h.pushes()
	h.noForge()

	changes, _ := h.publish(t, "added the changeset ingest")

	if len(changes.Repositories) != 2 {
		t.Fatalf("a run that touched two repositories reported %d", len(changes.Repositories))
	}

	sent := h.sent(t, channelv1.ChangeSetUpdated)
	if len(sent) == 0 {
		t.Fatal(
			"nothing was sent as the run finished, so norn would show an empty review screen for " +
				"work that is sitting on a branch",
		)
	}

	reported := decodeInto[channelv1.ChangeSet](t, sent[0])

	backend, found := repoNamed(reported, "backend")
	if !found {
		t.Fatalf("the backend is missing from what was sent: %+v", reported.Repos)
	}

	if backend.Commits != 3 || backend.Additions != 412 || backend.Deletions != 77 ||
		backend.Files != 9 {
		t.Fatalf(
			"the diffstat went out as %+v; a review screen sorts and totals these, so a wrong "+
				"number is worse than none",
			backend,
		)
	}

	if backend.Branch != entity.BranchFor("NORN-54", "backend", 1) {
		t.Fatalf("the branch went out as %q", backend.Branch)
	}

	if backend.BaseSHA != "base-backend" || backend.HeadSHA != "head-sha" {
		t.Fatalf("the revisions went out as %q..%q", backend.BaseSHA, backend.HeadSHA)
	}
}

func TestARepositoryTheRunNeverCommittedToIsLeftOffTheChangeSet(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(0, entity.Diffstat{})
	h.noForge()

	changes, _ := h.publish(t, "nothing needed changing")

	if len(changes.Repositories) != 0 {
		t.Fatalf(
			"a repository with no commits was reported anyway: %+v; the review screen would "+
				"offer a diff and a branch that hold nothing",
			changes.Repositories,
		)
	}

	if len(h.sent(t, channelv1.ChangeSetUpdated)) != 0 {
		t.Fatal("an empty change set was sent rather than nothing")
	}
}

func TestTheRunAlwaysSaysHowItFinishedEvenWhenItChangedNothing(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(0, entity.Diffstat{})
	h.noForge()

	h.publish(t, "there was nothing to do")

	sent := h.sent(t, channelv1.ExecutionResult)
	if len(sent) != 1 {
		t.Fatalf(
			"the run sent %d results; without one norn never learns the run finished and the "+
				"issue sits in review with no summary",
			len(sent),
		)
	}

	result := decodeInto[channelv1.Result](t, sent[0])

	if result.Summary != "there was nothing to do" {
		t.Fatalf("the summary went out as %q", result.Summary)
	}

	if result.Reported.IsZero() {
		t.Fatal(
			"the result carries no timestamp, so norn cannot tell it from an older report and " +
				"may keep the stale one",
		)
	}
}

func TestTheDiffIsKeptAsAnArtifactTheChangeSetPointsAt(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 1, Files: 1})
	h.pushes()
	h.noForge()

	var labels []string

	h.uploads.EXPECT().
		Attach(gomock.Any(), executionID, gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context, _, label string, body []byte,
		) (entity.ArtifactReceipt, error) {
			labels = append(labels, label)

			if len(body) == 0 {
				t.Error("an empty diff was uploaded")
			}

			return entity.ArtifactReceipt{ID: "f8b0a1c2-0000-4000-8000-000000000001"}, nil
		}).
		AnyTimes()

	changes, _ := h.publish(t, "one line")

	for _, change := range changes.Repositories {
		if change.DiffArtifact == "" {
			t.Fatalf(
				"%s reports no diff artifact, so View Diff on the review screen has nothing "+
					"behind it",
				change.Repository,
			)
		}
	}

	if len(labels) == 0 || !strings.HasSuffix(labels[0], ".diff.gz") {
		t.Fatalf("the diff was kept as %v, which does not say what it is", labels)
	}
}

func TestADiffTooLargeToKeepStillLeavesTheNumbersAndTheBranch(t *testing.T) {
	small := defaults()
	small.MaxDiffBytes = 1

	h := newHarness(t, small)
	h.changed(2, entity.Diffstat{Additions: 900000, Files: 400})
	h.pushes()
	h.noForge()

	changes, _ := h.publish(t, "a very large change")

	if len(changes.Repositories) == 0 {
		t.Fatal(
			"a run whose diff was too large reported nothing at all, so the work would look " +
				"like it never happened",
		)
	}

	for _, change := range changes.Repositories {
		if change.DiffArtifact != "" {
			t.Fatalf("%s kept a diff past the limit", change.Repository)
		}

		if change.Commits != 2 || change.Diffstat.Additions != 900000 {
			t.Fatalf("%s lost its numbers along with its diff: %+v", change.Repository, change)
		}
	}
}

func TestAPullRequestIsOpenedForEachBranchAndItsAddressIsReported(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 1, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")
	h.pushes()

	h.forges.EXPECT().
		Available(gomock.Any(), gomock.Any()).
		Return(entity.ForgeGitHub, true).
		AnyTimes()

	h.forges.EXPECT().
		Existing(gomock.Any(), gomock.Any(), gomock.Any()).
		Return("", nil).
		AnyTimes()

	var asked []entity.PullRequest

	h.forges.EXPECT().
		Open(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context, _ string, request entity.PullRequest,
		) (string, error) {
			asked = append(asked, request)

			return "https://github.com/usenorn/runner/pull/231", nil
		}).
		AnyTimes()

	_, publication := h.publish(t, "added a median helper")

	for _, held := range publication.Repositories {
		if held.PullRequest == "" || held.State != entity.PublicationPublished {
			t.Fatalf(
				"%s pushed a branch and opened nothing, so a person has to go and find the work "+
					"themselves",
				held.Repository,
			)
		}
	}

	if !publication.Complete() {
		t.Fatalf("a publication where every repository opened its pull request is %+v", publication)
	}

	if len(asked) != 2 {
		t.Fatalf("%d pull requests were opened for two repositories", len(asked))
	}

	if !strings.HasPrefix(asked[0].Title, "NORN-54") {
		t.Fatalf("the pull request is titled %q and does not name the issue", asked[0].Title)
	}
}

func TestASecondPassPutsItsCommitsOnThePullRequestThatIsAlreadyOpen(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(4, entity.Diffstat{Additions: 20, Files: 2})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")
	h.pushes()

	h.forges.EXPECT().
		Available(gomock.Any(), gomock.Any()).
		Return(entity.ForgeGitHub, true).
		AnyTimes()

	h.forges.EXPECT().
		Existing(gomock.Any(), gomock.Any(), gomock.Any()).
		Return("https://github.com/usenorn/runner/pull/231", nil).
		AnyTimes()

	h.forges.EXPECT().
		Open(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, entity.PullRequest) (string, error) {
			t.Error(
				"a second pull request was opened for a branch that already had one, so the " +
					"review would be split across two",
			)

			return "", errors.New("should not be reached")
		}).
		AnyTimes()

	_, publication := h.publish(t, "took the review feedback")

	for _, held := range publication.Repositories {
		if held.PullRequest != "https://github.com/usenorn/runner/pull/231" {
			t.Fatalf("%s reports %q", held.Repository, held.PullRequest)
		}
	}
}

func TestPushOnlyPushesTheBranchAndOpensNothing(t *testing.T) {
	pushOnly := defaults()
	pushOnly.CreatePRs = config.PullRequestsPushOnly

	h := newHarness(t, pushOnly)
	h.changed(1, entity.Diffstat{Additions: 1, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")

	h.pushes()

	h.forges.EXPECT().
		Available(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (entity.ForgeKind, bool) {
			t.Error("push_only went looking for a pull request tool")

			return "", false
		}).
		AnyTimes()

	_, publication := h.publish(t, "pushed only")

	if len(h.pushed) != 2 {
		t.Fatalf("%d branches were pushed and the run touched two", len(h.pushed))
	}

	for _, held := range publication.Repositories {
		if held.PullRequest != "" {
			t.Fatalf("%s opened a pull request under push_only", held.Repository)
		}
	}

	if !publication.Complete() {
		t.Fatalf("a push_only publication that pushed everything is %+v", publication)
	}
}

func TestARepositoryThatCouldNotBePushedNeverClaimsAPullRequest(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 1, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")

	h.forges.EXPECT().
		Available(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (entity.ForgeKind, bool) {
			t.Error(
				"a repository that was never pushed was still offered to the forge, which can " +
					"only open a pull request for a branch the remote has",
			)

			return "", false
		}).
		AnyTimes()

	_, publication := h.publish(t, "nowhere to push")

	if len(publication.Repositories) != 2 {
		t.Fatalf(
			"work that could not be pushed vanished from the report: %+v; it is still on this "+
				"machine and somebody has to be told where",
			publication.Repositories,
		)
	}

	for _, held := range publication.Repositories {
		if held.PullRequest != "" || held.State != entity.PublicationFailed || held.Step != entity.PublicationStepPush {
			t.Fatalf("%s came back as %+v after it could not be pushed", held.Repository, held)
		}
	}

	if publication.Complete() {
		t.Fatal("a publication that pushed nothing reads as complete")
	}
}

func TestTheProvisionalCommitCarryingSomebodyElsesWorkIsNotCountedAsTheRuns(t *testing.T) {
	h := newHarness(t, defaults())
	h.snapshot.Repositories[0].Local = &entity.LocalPatch{
		BaseSHA: "base-backend",
		Commit:  "provisional-sha",
	}

	var bases []string

	h.worktrees.EXPECT().Head(gomock.Any(), gomock.Any()).Return("head-sha", nil).AnyTimes()

	h.worktrees.EXPECT().
		Commits(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, base, _ string) (int, error) {
			bases = append(bases, base)

			return 0, nil
		}).
		AnyTimes()

	h.noForge()

	h.publish(t, "nothing")

	if len(bases) == 0 || bases[0] != "provisional-sha" {
		t.Fatalf(
			"the run was measured from %v; the first of those is where the operator's own "+
				"uncommitted work was laid down, and counting from before it would report their "+
				"changes as the coding agent's",
			bases,
		)
	}
}

func repoNamed(changes channelv1.ChangeSet, name string) (channelv1.RepoChange, bool) {
	for _, change := range changes.Repos {
		if change.Repository == name {
			return change, true
		}
	}

	return channelv1.RepoChange{}, false
}

func TestNothingLeavesTheMachineWhileTheChangesWaitForReview(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 1, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")

	h.worktrees.EXPECT().Push(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	h.forges.EXPECT().Open(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	changes := h.collect(t, "ready for review")

	if len(changes.Repositories) == 0 {
		t.Fatal("a run that committed work reported nothing to review")
	}
}

func TestARetryPublishesOnlyWhatIsLeftAndNeverOpensASecondPullRequest(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 1, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")
	h.pushes()

	h.forges.EXPECT().Available(gomock.Any(), gomock.Any()).Return(entity.ForgeGitHub, true).AnyTimes()
	h.forges.EXPECT().Existing(gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

	opened := map[string]int{}
	refusing := true

	h.forges.EXPECT().
		Open(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, dir string, _ entity.PullRequest) (string, error) {
			opened[dir]++

			if refusing && strings.HasSuffix(dir, "frontend") {
				return "", errors.New("HTTP 502")
			}

			return "https://github.com/usenorn/runner/pull/231", nil
		}).
		AnyTimes()

	changes, first := h.publish(t, "added a median helper")

	if first.Complete() || len(first.Failures()) != 1 {
		t.Fatalf("a publication where one pull request failed reads as %+v", first)
	}

	refusing = false
	second := h.republish(t, entity.ReviewOf(1, "added a median helper", h.snapshot, changes))

	if !second.Complete() {
		t.Fatalf("the retry left %+v", second)
	}

	if len(h.pushed) != 2 {
		t.Fatalf("%d pushes for two repositories across two attempts; a retry pushed again", len(h.pushed))
	}

	for dir, count := range opened {
		if strings.HasSuffix(dir, "backend") && count != 1 {
			t.Fatalf("the backend's pull request was opened %d times", count)
		}
	}

	if sent := h.sent(t, channelv1.PublicationUpdated); len(sent) == 0 {
		t.Fatal("norn was never told how each repository's publication went")
	}
}

func TestTheReviewedCommitIsPushedWhateverTheBranchHoldsNow(t *testing.T) {
	h := newHarness(t, defaults())
	h.changed(1, entity.Diffstat{Additions: 1, Files: 1})
	h.keeps("f8b0a1c2-0000-4000-8000-000000000001")
	h.pushes()
	h.noForge()

	changes := h.collect(t, "added a median helper")
	review := entity.ReviewOf(1, "added a median helper", h.snapshot, changes)

	for index := range review.Repositories {
		review.Repositories[index].HeadSHA = "reviewed-" + review.Repositories[index].Name
	}

	h.republish(t, review)

	for _, push := range h.pushed {
		if !strings.HasPrefix(push.SHA, "reviewed-") || !push.Lease.Known {
			t.Fatalf("the machine pushed %+v rather than the reviewed commit under a lease", push)
		}
	}
}
