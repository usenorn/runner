package entity_test

import (
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

const pullRequest = "https://github.com/usenorn/runner/pull/231"

func TestOnlyWhatAppearedSinceTheLastLookIsNews(t *testing.T) {
	old := entity.PullRequestComment{Kind: entity.CommentConversation, ID: "1", Body: "looks good"}
	status := entity.PullRequestStatus{URL: pullRequest, Head: "abc", Comments: []entity.PullRequestComment{old}}

	cursor := entity.WatchCursor{}.Absorb(status, entity.PullRequestNews{})

	fresh := entity.PullRequestComment{Kind: entity.CommentInline, ID: "2", Body: "rename this"}
	status.Comments = append(status.Comments, fresh)
	status.Failed = []entity.FailedCheck{{Name: "test"}}

	news := entity.NewsOf(status, entity.RemoteState{DefaultTip: "base1"}, cursor)

	if len(news.Comments) != 1 || news.Comments[0].ID != "2" || len(news.Failed) != 1 {
		t.Fatalf("the news reads %+v, want the new comment and the failed check", news)
	}

	cursor = cursor.Absorb(status, news)

	if again := entity.NewsOf(status, entity.RemoteState{DefaultTip: "base1"}, cursor); !again.Empty() {
		t.Fatalf("the same comment and check came back as news a second time: %+v", again)
	}

	status.Head = "def"

	if rerun := entity.NewsOf(status, entity.RemoteState{DefaultTip: "base1"}, cursor); len(rerun.Failed) != 1 {
		t.Fatalf("the check failing again on a new head was not news: %+v", rerun)
	}
}

func TestAConflictIsNewsOncePerBranchAndBasePair(t *testing.T) {
	status := entity.PullRequestStatus{URL: pullRequest, Head: "abc"}
	refreshed := entity.RemoteState{DefaultTip: "base1", Conflicts: []string{"stats.go"}}

	news := entity.NewsOf(status, refreshed, entity.WatchCursor{Baselined: true})
	if len(news.Conflicts) != 1 {
		t.Fatalf("a conflict with the base branch was not news: %+v", news)
	}

	cursor := entity.WatchCursor{Baselined: true}.Absorb(status, news)

	if again := entity.NewsOf(status, refreshed, cursor); !again.Empty() {
		t.Fatalf("an unchanged conflict brought the run back a second time: %+v", again)
	}

	refreshed.DefaultTip = "base2"

	if moved := entity.NewsOf(status, refreshed, cursor); len(moved.Conflicts) != 1 {
		t.Fatalf("a conflict with a base that moved again was not news: %+v", moved)
	}
}

func TestTheRunEndsOnlyWhenEveryPullRequestIsMergedOrOneIsClosed(t *testing.T) {
	cases := []struct {
		states []entity.PullRequestState
		want   entity.WatchOutcome
	}{
		{[]entity.PullRequestState{entity.PullRequestMerged, entity.PullRequestOpen}, entity.WatchGoesOn},
		{[]entity.PullRequestState{entity.PullRequestMerged, entity.PullRequestMerged}, entity.WatchMerged},
		{[]entity.PullRequestState{entity.PullRequestMerged, entity.PullRequestClosed}, entity.WatchClosed},
	}

	for _, tc := range cases {
		statuses := make([]entity.PullRequestStatus, 0, len(tc.states))
		for _, state := range tc.states {
			statuses = append(statuses, entity.PullRequestStatus{State: state})
		}

		if got := entity.OutcomeOf(statuses); got != tc.want {
			t.Fatalf("%v ends as %s, want %s", tc.states, got, tc.want)
		}
	}
}

func TestAThreadIdCarriesEverythingNeededToAnswerOnTheRightPullRequest(t *testing.T) {
	reply := entity.PullRequestComment{Kind: entity.CommentInline, ID: "300", Parent: "200"}

	thread, err := entity.ParsePullRequestThread(reply.Thread(pullRequest))
	if err != nil {
		t.Fatalf("read the thread back: %v", err)
	}

	if thread.Kind != entity.CommentInline || thread.ID != "200" || thread.PullRequest != pullRequest {
		t.Fatalf("the thread reads %+v; a reply to a reply has to answer the thread it started in", thread)
	}

	if _, err := entity.ParsePullRequestThread("01J0NORNTHREAD"); err == nil {
		t.Fatal("a norn review thread read as a pull request thread")
	}
}

func TestTheBriefingTellsTheAgentWhatToDoWithEachKindOfNews(t *testing.T) {
	briefing := entity.PullRequestBriefing([]entity.PullRequestNews{{
		Repository: "runner", URL: pullRequest,
		Conflicts:  []string{"stats.go"},
		Failed:     []entity.FailedCheck{{Name: "test", Log: "expected 2.5"}},
		Comments:   []entity.PullRequestComment{{Kind: entity.CommentInline, ID: "200", Body: "rename", Path: "a.go", Line: 3}},
	}})

	for _, wanted := range []string{"conflicts with the base branch in: stats.go", "expected 2.5", "a.go line 3", "reply_to_review", "complete_task"} {
		if !strings.Contains(briefing, wanted) {
			t.Fatalf("the briefing never says %q:\n%s", wanted, briefing)
		}
	}
}
