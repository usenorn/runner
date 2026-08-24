package changeset

import (
	"bytes"
	"compress/gzip"
	"context"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
)

func (s *changeSetsService) deliver(
	ctx context.Context,
	execution entity.Execution,
	snapshot entity.Snapshot,
	changes entity.ChangeSet,
	completion entity.Completion,
	previews []entity.PreviewLink,
) entity.ChangeSet {
	sources := make(map[string]entity.SnapshotRepository, len(snapshot.Repositories))

	for _, held := range snapshot.Repositories {
		sources[held.Name] = held
	}

	for index, change := range changes.Repositories {
		held, known := sources[change.Repository]
		if !known {
			continue
		}

		if !s.push(ctx, execution, held, change) {
			continue
		}

		if s.results.CreatePRs != config.PullRequestsAuto {
			continue
		}

		changes.Repositories[index].PullRequest = s.request(
			ctx, execution, held, change, completion, previews,
		)
	}

	return changes
}

func (s *changeSetsService) push(
	ctx context.Context,
	execution entity.Execution,
	held entity.SnapshotRepository,
	change entity.RepositoryChange,
) bool {
	if change.Branch != held.Branch {
		s.tell(ctx, execution.ID, entity.PushRefused(held.Name, change.Branch, entity.ErrBranchMoved))

		return false
	}

	url, err := s.worktrees.Remote(ctx, held.Source)
	if err != nil {
		s.tell(ctx, execution.ID, entity.PushSkipped(held.Name, err))

		return false
	}

	if err := s.worktrees.Push(ctx, held.Path, url, change.Branch); err != nil {
		s.tell(ctx, execution.ID, entity.PushRefused(held.Name, change.Branch, err))

		return false
	}

	s.tell(ctx, execution.ID, entity.Pushed(held.Name, change.Branch))

	return true
}

func (s *changeSetsService) request(
	ctx context.Context,
	execution entity.Execution,
	held entity.SnapshotRepository,
	change entity.RepositoryChange,
	completion entity.Completion,
	previews []entity.PreviewLink,
) string {
	if _, available := s.forges.Available(ctx, held.Path); !available {
		s.tell(ctx, execution.ID, entity.PullRequestSkipped(held.Name))

		return ""
	}

	if already, err := s.forges.Existing(ctx, held.Path, change.Branch); err == nil && already != "" {
		s.tell(ctx, execution.ID, entity.PullRequestAmended(held.Name, already))

		return already
	}

	title, fromTitle := entity.ScrubbedForForge(
		entity.PullRequestTitle(execution.IssueKey, execution.Title),
	)

	body, fromBody := entity.ScrubbedForForge(entity.PullRequestBody(
		execution.IssueKey, execution.Title, completion, change, previews,
		s.results.Attribution == config.AttributionStandard,
	))

	if scrubbed := entity.Scrubs(fromTitle, fromBody); len(scrubbed) > 0 {
		s.tell(ctx, execution.ID, entity.PullRequestScrubbed(held.Name, scrubbed))
	}

	address, err := s.forges.Open(ctx, held.Path, entity.PullRequest{
		Title:  title,
		Body:   body,
		Branch: change.Branch,
	})
	if err != nil {
		s.tell(ctx, execution.ID, entity.PullRequestRefused(held.Name, err))

		return ""
	}

	s.tell(ctx, execution.ID, entity.PullRequestOpened(held.Name, address))

	return address
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
