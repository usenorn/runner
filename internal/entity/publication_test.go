package entity_test

import (
	"errors"
	"slices"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func reviewedTwice() entity.Review {
	return entity.Review{
		Revision: 2,
		Repositories: []entity.ReviewedRepository{
			{Name: "api", Branch: "norn/NORN-231/api", HeadSHA: "a2"},
			{Name: "web", Branch: "norn/NORN-231/web", HeadSHA: "w2"},
		},
	}
}

func TestAnApprovalHoldsOnlyForTheExactRevisionAndHeadsThatWereReviewed(t *testing.T) {
	approved := channelv1.Instruction{Revision: 2, Heads: map[string]string{"api": "a2", "web": "w2"}}
	live := map[string]string{"api": "a2", "web": "w2"}

	for name, tc := range map[string]struct {
		review      entity.Review
		instruction channelv1.Instruction
		live        map[string]string
		want        error
	}{
		"exactly what was reviewed": {reviewedTwice(), approved, live, nil},
		"an earlier revision": {
			reviewedTwice(),
			channelv1.Instruction{Revision: 1, Heads: approved.Heads},
			live, entity.ErrApprovalStale,
		},
		"a head nobody reviewed": {
			reviewedTwice(),
			channelv1.Instruction{Revision: 2, Heads: map[string]string{"api": "a3", "web": "w2"}},
			live, entity.ErrApprovalStale,
		},
		"one repository left out": {
			reviewedTwice(),
			channelv1.Instruction{Revision: 2, Heads: map[string]string{"api": "a2"}},
			live, entity.ErrApprovalStale,
		},
		"a branch that moved after review": {
			reviewedTwice(), approved, map[string]string{"api": "a2", "web": "w3"}, entity.ErrApprovalMoved,
		},
		"nothing written down": {entity.Review{}, approved, live, entity.ErrReviewMissing},
	} {
		t.Run(name, func(t *testing.T) {
			if got := entity.ApprovalRefusal(tc.review, tc.instruction, tc.live); !errors.Is(got, tc.want) &&
				(tc.want != nil || got != nil) {
				t.Fatalf("the approval came back %v, want %v", got, tc.want)
			}
		})
	}
}

func TestARetryKeepsWhatWasPublishedAndStartsOverOnlyWhatWasNot(t *testing.T) {
	previous := entity.Publication{
		Revision: 2,
		Attempt:  1,
		Repositories: []entity.RepositoryPublication{
			{Repository: "api", SHA: "a2", State: entity.PublicationPublished, PullRequest: "https://x/1"},
			{Repository: "web", SHA: "w2", State: entity.PublicationFailed, Failure: "HTTP 502"},
		},
	}

	next := entity.PublicationOf(reviewedTwice(), previous)

	api, _ := next.Of("api")
	web, _ := next.Of("web")

	if next.Attempt != 2 || api.State != entity.PublicationPublished || api.PullRequest != "https://x/1" {
		t.Fatalf("a retry forgot what was already published: %+v", next)
	}

	if web.State != entity.PublicationPending || web.Failure != "" {
		t.Fatalf("a retry did not start the failed repository over: %+v", web)
	}

	fresh := entity.PublicationOf(entity.Review{
		Revision:     3,
		Repositories: []entity.ReviewedRepository{{Name: "api", HeadSHA: "a3"}},
	}, previous)

	if held, _ := fresh.Of("api"); fresh.Attempt != 1 || held.State != entity.PublicationPending {
		t.Fatalf("a new revision inherited an older one's publication: %+v", fresh)
	}
}

func TestAPushNamesTheReviewedCommitAndTheLeaseItTook(t *testing.T) {
	for name, tc := range map[string]struct {
		lease entity.Lease
		want  string
	}{
		"the branch was absent":   {entity.Lease{Known: true}, "--force-with-lease=refs/heads/b:"},
		"the branch had a tip":    {entity.Lease{Known: true, Tip: "t1"}, "--force-with-lease=refs/heads/b:t1"},
		"nobody could say either": {entity.Lease{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			args := entity.Push{SHA: "a2", Branch: "b", Lease: tc.lease}.Arguments("origin-url")

			if args[len(args)-1] != "a2:refs/heads/b" || !slices.Contains(args, "--no-verify") {
				t.Fatalf("the push asked for %v", args)
			}

			leased := slices.ContainsFunc(args, func(arg string) bool { return arg == tc.want })
			if tc.want != "" && !leased {
				t.Fatalf("the push asked for %v, without %s", args, tc.want)
			}

			if tc.want == "" && slices.ContainsFunc(args, func(arg string) bool {
				return len(arg) > 18 && arg[:18] == "--force-with-lease"
			}) {
				t.Fatalf("a push with no known lease may overwrite the branch: %v", args)
			}
		})
	}
}
