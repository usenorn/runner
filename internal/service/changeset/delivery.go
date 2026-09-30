package changeset

import (
	"bytes"
	"compress/gzip"
	"context"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
)

func (s *changeSetsService) Publish(
	ctx context.Context,
	execution entity.Execution,
	review entity.Review,
) (entity.Publication, error) {
	previous, err := s.runs.LoadPublication(ctx, execution.ID)
	if err != nil {
		return entity.Publication{}, err
	}

	publication := entity.PublicationOf(review, previous)

	if err := s.record(ctx, execution.ID, publication); err != nil {
		return publication, err
	}

	for _, repository := range review.Repositories {
		held, _ := publication.Of(repository.Name)
		if held.State == entity.PublicationPublished {
			continue
		}

		publication.Record(s.deliver(ctx, execution, repository, held))

		if err := s.record(ctx, execution.ID, publication); err != nil {
			return publication, err
		}
	}

	return publication, nil
}

func (s *changeSetsService) Tips(
	ctx context.Context,
	review entity.Review,
) (map[string]string, error) {
	tips := make(map[string]string, len(review.Repositories))

	for _, repository := range review.Repositories {
		tip, err := s.worktrees.Resolve(ctx, repository.Path, "refs/heads/"+repository.Branch)
		if err != nil {
			return nil, err
		}

		tips[repository.Name] = tip
	}

	return tips, nil
}

func (s *changeSetsService) record(
	ctx context.Context,
	executionID string,
	publication entity.Publication,
) error {
	if err := s.runs.SavePublication(ctx, executionID, publication); err != nil {
		return err
	}

	s.send(ctx, channelv1.PublicationUpdated, executionID, publication.Wire(s.now()))

	return nil
}

func (s *changeSetsService) deliver(
	ctx context.Context,
	execution entity.Execution,
	repository entity.ReviewedRepository,
	held entity.RepositoryPublication,
) entity.RepositoryPublication {
	if err := s.push(ctx, repository); err != nil {
		s.tell(ctx, execution.ID, entity.PushRefused(repository.Name, repository.Branch, err))

		return failed(held, entity.PublicationStepPush, err)
	}

	s.tell(ctx, execution.ID, entity.Pushed(repository.Name, repository.Branch))

	held.State, held.Step, held.Failure = entity.PublicationPushed, entity.PublicationStepPush, ""

	if s.results.CreatePRs != config.PullRequestsAuto {
		held.State = entity.PublicationPublished

		return held
	}

	address, err := s.request(ctx, execution, repository)
	if err != nil {
		s.tell(ctx, execution.ID, entity.PullRequestRefused(repository.Name, err))

		return failed(held, entity.PublicationStepPullRequest, err)
	}

	held.State, held.Step, held.PullRequest = entity.PublicationPublished, entity.PublicationStepPullRequest, address

	return held
}

func failed(
	held entity.RepositoryPublication,
	step entity.PublicationStep,
	err error,
) entity.RepositoryPublication {
	held.State, held.Step, held.Failure = entity.PublicationFailed, step, err.Error()

	return held
}

func (s *changeSetsService) push(ctx context.Context, repository entity.ReviewedRepository) error {
	if repository.Remote == "" {
		return entity.ErrPushNowhere
	}

	tip, err := s.worktrees.RemoteTip(ctx, repository.Remote, repository.Branch)
	if err != nil {
		return err
	}

	if tip == repository.HeadSHA {
		return nil
	}

	return s.worktrees.Push(ctx, repository.Path, repository.Remote, entity.Push{
		SHA:    repository.HeadSHA,
		Branch: repository.Branch,
		Lease:  repository.Lease,
	})
}

func (s *changeSetsService) request(
	ctx context.Context,
	execution entity.Execution,
	repository entity.ReviewedRepository,
) (string, error) {
	if _, available := s.forges.Available(ctx, repository.Path); !available {
		return "", entity.ErrForgeAbsent
	}

	already, err := s.forges.Existing(ctx, repository.Path, repository.Branch)
	if err != nil {
		return "", err
	}

	if already != "" {
		s.tell(ctx, execution.ID, entity.PullRequestAmended(repository.Name, already))

		return already, nil
	}

	title, scrubbed := entity.ScrubbedForForge(
		entity.PullRequestTitle(execution.IssueKey, execution.Title),
	)

	if len(scrubbed) > 0 {
		s.tell(ctx, execution.ID, entity.PullRequestScrubbed(repository.Name, scrubbed))
	}

	address, err := s.forges.Open(ctx, repository.Path, entity.PullRequest{
		Title:  title,
		Branch: repository.Branch,
	})
	if err != nil {
		return "", err
	}

	s.tell(ctx, execution.ID, entity.PullRequestOpened(repository.Name, address))

	return address, nil
}

func squeeze(patch []byte) ([]byte, error) {
	var packed bytes.Buffer

	writer := gzip.NewWriter(&packed)

	if _, err := writer.Write(patch); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return packed.Bytes(), nil
}
