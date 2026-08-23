package changeset

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

type changeSetsService struct {
	runs      repository.Run
	spool     repository.Spool
	worktrees repository.Worktree
	forges    repository.Forge
	uploads   service.Uploads
	results   config.Results
	now       func() time.Time
}

func New(
	runs repository.Run,
	spool repository.Spool,
	worktrees repository.Worktree,
	forges repository.Forge,
	uploads service.Uploads,
	results config.Results,
) service.ChangeSets {
	return &changeSetsService{
		runs:      runs,
		spool:     spool,
		worktrees: worktrees,
		forges:    forges,
		uploads:   uploads,
		results:   results,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (s *changeSetsService) Uncommitted(
	ctx context.Context,
	snapshot entity.Snapshot,
) ([]entity.UncommittedWork, error) {
	left := make([]entity.UncommittedWork, 0, len(snapshot.Repositories))

	for _, held := range snapshot.Repositories {
		changed, err := s.worktrees.Changed(ctx, held.Path)
		if err != nil {
			return nil, err
		}

		untracked, err := s.worktrees.Untracked(ctx, held.Path)
		if err != nil {
			return nil, err
		}

		files := append(changed, untracked...)
		if len(files) == 0 {
			continue
		}

		left = append(left, entity.UncommittedWork{Repository: held.Name, Files: files})
	}

	return left, nil
}

func (s *changeSetsService) Publish(
	ctx context.Context,
	execution entity.Execution,
	snapshot entity.Snapshot,
	completion entity.Completion,
) (entity.ChangeSet, error) {
	changes, err := s.collect(ctx, execution, snapshot)
	if err != nil {
		return entity.ChangeSet{}, err
	}

	if beyond := changes.Beyond(); beyond > 0 {
		s.tell(ctx, execution.ID, entity.ChangeSetOverflow(beyond))
	}

	s.report(ctx, execution.ID, changes)

	changes = s.deliver(ctx, execution, snapshot, changes, completion)

	s.report(ctx, execution.ID, changes)

	return changes, s.settle(ctx, execution.ID, completion, changes)
}

func (s *changeSetsService) collect(
	ctx context.Context,
	execution entity.Execution,
	snapshot entity.Snapshot,
) (entity.ChangeSet, error) {
	changes := entity.ChangeSet{Repositories: make([]entity.RepositoryChange, 0, len(snapshot.Repositories))}

	for _, held := range snapshot.Repositories {
		base := startOf(held)

		commits, err := s.worktrees.Commits(ctx, held.Path, base)
		if err != nil {
			return entity.ChangeSet{}, err
		}

		if commits == 0 {
			continue
		}

		head, err := s.worktrees.Head(ctx, held.Path)
		if err != nil {
			return entity.ChangeSet{}, err
		}

		stat, err := s.worktrees.Diffstat(ctx, held.Path, base)
		if err != nil {
			return entity.ChangeSet{}, err
		}

		changes.Repositories = append(changes.Repositories, entity.RepositoryChange{
			Repository:   held.Name,
			Branch:       held.Branch,
			BaseSHA:      base,
			HeadSHA:      head,
			Commits:      commits,
			Diffstat:     stat,
			DiffArtifact: s.keepDiff(ctx, execution, held, base),
		})
	}

	return changes, nil
}

func startOf(held entity.SnapshotRepository) string {
	if held.Local != nil && held.Local.Commit != "" {
		return held.Local.Commit
	}

	return held.BaseSHA
}

func (s *changeSetsService) keepDiff(
	ctx context.Context,
	execution entity.Execution,
	held entity.SnapshotRepository,
	base string,
) string {
	patch, err := s.worktrees.Patch(ctx, held.Path, base)
	if err != nil {
		s.tell(ctx, execution.ID, entity.DiffUnreadable(held.Name, err))

		return ""
	}

	packed, err := squeeze(patch)
	if err != nil {
		s.tell(ctx, execution.ID, entity.DiffUnreadable(held.Name, err))

		return ""
	}

	if int64(len(packed)) > s.results.MaxDiffBytes {
		s.tell(ctx, execution.ID, entity.DiffTooLarge(held.Name, int64(len(packed)), s.results.MaxDiffBytes))

		return ""
	}

	receipt, err := s.uploads.Attach(ctx, execution.ID, entity.DiffLabel(held.Name), packed)
	if err != nil {
		s.tell(ctx, execution.ID, entity.DiffUnkept(held.Name, err))

		return ""
	}

	return receipt.ID
}

func (s *changeSetsService) report(
	ctx context.Context,
	executionID string,
	changes entity.ChangeSet,
) {
	if changes.Empty() {
		return
	}

	s.send(ctx, channelv1.ChangeSetUpdated, executionID, changes.Wire())
}

func (s *changeSetsService) settle(
	ctx context.Context,
	executionID string,
	completion entity.Completion,
	changes entity.ChangeSet,
) error {
	raw, err := json.Marshal(entity.ResultOf(completion.Summary, changes, s.now()))
	if err != nil {
		return err
	}

	message, err := channelv1.NewRunnerMessage(
		channelv1.ExecutionResult, executionID, raw, s.now(),
	)
	if err != nil {
		return err
	}

	return s.spool.Append(context.WithoutCancel(ctx), message)
}

func (s *changeSetsService) send(
	ctx context.Context,
	kind channelv1.MessageType,
	executionID string,
	payload any,
) {
	ctx = context.WithoutCancel(ctx)

	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}

	message, err := channelv1.NewRunnerMessage(kind, executionID, raw, s.now())
	if err != nil {
		return
	}

	if err := s.spool.Append(ctx, message); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not tell norn what a run changed",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)
	}
}

func (s *changeSetsService) tell(ctx context.Context, executionID string, reason string) {
	ctx = context.WithoutCancel(ctx)

	entry := entity.TimelineEntry{
		Kind:     channelv1.EventNote,
		Reason:   reason,
		Occurred: s.now(),
	}

	if err := s.runs.Append(ctx, executionID, entry); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not add a line to a run's own timeline",
			slog.String("execution_id", executionID),
			slog.String("error", err.Error()),
		)
	}

	s.send(ctx, channelv1.ExecutionEvent, executionID, channelv1.Entry{
		Kind:     string(channelv1.EventNote),
		Reason:   reason,
		Occurred: entry.Occurred,
	})
}
