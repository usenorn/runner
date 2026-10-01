package entity

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

const (
	RunReviewFile      = "review.json"
	RunApprovalFile    = "approval.json"
	RunPublicationFile = "publish.json"
)

var (
	ErrApprovalStale   = errors.New("the approval names changes other than the ones this run holds")
	ErrApprovalMoved   = errors.New("a branch moved on after its changes were reviewed")
	ErrApprovalMissing = errors.New("this run never wrote down which changes were approved")
	ErrReviewMissing   = errors.New("this run never wrote down what was reviewed")
	ErrBranchDiverged  = errors.New(
		"the branch has commits on the remote that this run does not have, and pushing would " +
			"throw them away; ask for changes so the run merges them in, then approve again",
	)
)

type ReviewedRepository struct {
	Name    string
	Branch  string
	Path    string
	HeadSHA string
	Remote  string
	Lease   Lease
}

type Review struct {
	Revision     int
	Summary      string
	Repositories []ReviewedRepository
}

func ReviewOf(
	revision int,
	summary string,
	snapshot Snapshot,
	changes ChangeSet,
) Review {
	held := make(map[string]SnapshotRepository, len(snapshot.Repositories))

	for _, repository := range snapshot.Repositories {
		held[repository.Name] = repository
	}

	review := Review{Revision: revision, Summary: summary}

	for _, change := range changes.Repositories {
		source := held[change.Repository]

		review.Repositories = append(review.Repositories, ReviewedRepository{
			Name:    change.Repository,
			Branch:  change.Branch,
			Path:    source.Path,
			HeadSHA: change.HeadSHA,
			Remote:  source.Remote,
			Lease:   source.Lease,
		})
	}

	return review
}

func (r Review) Heads() map[string]string {
	heads := make(map[string]string, len(r.Repositories))

	for _, repository := range r.Repositories {
		heads[repository.Name] = repository.HeadSHA
	}

	return heads
}

func ApprovalRefusal(
	review Review,
	instruction channelv1.Instruction,
	live map[string]string,
) error {
	if review.Revision == 0 {
		return ErrReviewMissing
	}

	if instruction.Revision != review.Revision || !maps.Equal(instruction.Heads, review.Heads()) {
		return fmt.Errorf(
			"%w: revision %d was approved and revision %d is held",
			ErrApprovalStale, instruction.Revision, review.Revision,
		)
	}

	for _, repository := range review.Repositories {
		if tip := live[repository.Name]; tip != repository.HeadSHA {
			return fmt.Errorf(
				"%w: %s was reviewed at %s and now points at %s",
				ErrApprovalMoved, repository.Branch, ShortSHA(repository.HeadSHA), ShortSHA(tip),
			)
		}
	}

	return nil
}

type PublicationStep string

const (
	PublicationStepPush        PublicationStep = channelv1.PublicationStepPush
	PublicationStepPullRequest PublicationStep = channelv1.PublicationStepPullRequest
)

type PublicationState string

const (
	PublicationPending   PublicationState = channelv1.PublicationPending
	PublicationPushed    PublicationState = channelv1.PublicationPushed
	PublicationPublished PublicationState = channelv1.PublicationPublished
	PublicationFailed    PublicationState = channelv1.PublicationFailed
)

type RepositoryPublication struct {
	Repository  string
	Branch      string
	SHA         string
	State       PublicationState
	Step        PublicationStep
	Failure     string
	PullRequest string
}

type Publication struct {
	Revision     int
	Attempt      int
	Repositories []RepositoryPublication
}

func PublicationOf(review Review, previous Publication) Publication {
	publication := Publication{Revision: review.Revision, Attempt: 1}

	if previous.Revision == review.Revision {
		publication.Attempt = previous.Attempt + 1
	}

	for _, repository := range review.Repositories {
		held, found := previous.Of(repository.Name)
		if !found || previous.Revision != review.Revision || held.SHA != repository.HeadSHA {
			held = RepositoryPublication{
				Repository: repository.Name,
				Branch:     repository.Branch,
				SHA:        repository.HeadSHA,
			}
		}

		if held.State != PublicationPublished {
			held.State = PublicationPending
			held.Failure = ""
		}

		publication.Repositories = append(publication.Repositories, held)
	}

	return publication
}

func (p Publication) Of(repository string) (RepositoryPublication, bool) {
	for _, held := range p.Repositories {
		if held.Repository == repository {
			return held, true
		}
	}

	return RepositoryPublication{}, false
}

func (p *Publication) Record(outcome RepositoryPublication) {
	for index, held := range p.Repositories {
		if held.Repository == outcome.Repository {
			p.Repositories[index] = outcome

			return
		}
	}

	p.Repositories = append(p.Repositories, outcome)
}

func (p Publication) Complete() bool {
	for _, held := range p.Repositories {
		if held.State != PublicationPublished {
			return false
		}
	}

	return true
}

func (p Publication) Failures() []RepositoryPublication {
	failed := make([]RepositoryPublication, 0, len(p.Repositories))

	for _, held := range p.Repositories {
		if held.State == PublicationFailed {
			failed = append(failed, held)
		}
	}

	return failed
}

func (p Publication) Wire(reported time.Time) channelv1.Publication {
	repos := make([]channelv1.RepoPublication, 0, len(p.Repositories))

	for _, held := range p.Repositories {
		repos = append(repos, channelv1.RepoPublication{
			Repository:  held.Repository,
			Branch:      held.Branch,
			SHA:         held.SHA,
			State:       string(held.State),
			Step:        string(held.Step),
			Failure:     held.Failure,
			PullRequest: held.PullRequest,
		})
	}

	return channelv1.Publication{
		Revision: p.Revision, Attempt: p.Attempt, Repos: repos, Reported: reported,
	}
}

func PublicationIncomplete(failed []RepositoryPublication) string {
	named := make([]string, 0, len(failed))

	for _, held := range failed {
		named = append(named, held.Repository)
	}

	return "publication is incomplete: " + strings.Join(named, ", ") +
		" could not be published, and the run waits for somebody to retry or give up"
}

func PublicationAbandoned() string {
	return "somebody gave up on publishing this run's approved changes"
}

func ApprovalRefused(err error) string {
	return fmt.Sprintf("the approval was refused and the changes go back for review, because %s", err)
}

type Push struct {
	SHA    string
	Branch string
	Lease  Lease
}

func (p Push) Arguments(url string) []string {
	ref := "refs/heads/" + p.Branch
	args := []string{"push", "--quiet", "--no-verify"}

	if p.Lease.Known {
		args = append(args, "--force-with-lease="+ref+":"+p.Lease.Tip)
	}

	return append(args, url, p.SHA+":"+ref)
}
