package snapshot

import (
	"context"

	"github.com/usenorn/runner/internal/entity"
)

func (s *snapshotsService) Refresh(ctx context.Context, snapshot entity.Snapshot) []entity.RemoteState {
	states := make([]entity.RemoteState, 0, len(snapshot.Repositories))

	for _, repository := range snapshot.Repositories {
		if repository.Remote == "" {
			continue
		}

		states = append(states, s.refresh(ctx, repository))
	}

	return states
}

func (s *snapshotsService) refresh(ctx context.Context, repository entity.SnapshotRepository) entity.RemoteState {
	state := entity.RemoteState{
		Repository: repository.RelPath,
		Default:    repository.Default,
		Branch:     repository.Branch,
	}

	if state.Default == "" {
		named, err := s.worktrees.RemoteDefault(ctx, repository.Source)
		if err != nil {
			state.Failure = err.Error()

			return state
		}

		state.Default = named
	}

	if err := s.worktrees.Fetch(ctx, repository.Source, state.Default); err != nil {
		state.Failure = err.Error()

		return state
	}

	pushed, err := s.worktrees.FetchIfPresent(ctx, repository.Source, repository.Branch)
	if err != nil {
		state.Failure = err.Error()

		return state
	}

	if repository.Mode == entity.GitModeClone {
		if err := s.mirror(ctx, repository, state.Default, pushed); err != nil {
			state.Failure = err.Error()

			return state
		}
	}

	return s.compare(ctx, repository, state, pushed)
}

func (s *snapshotsService) mirror(
	ctx context.Context,
	repository entity.SnapshotRepository,
	defaultBranch string,
	pushed bool,
) error {
	if err := s.worktrees.Mirror(ctx, repository.Path, repository.Source, defaultBranch); err != nil {
		return err
	}

	if !pushed {
		return nil
	}

	return s.worktrees.Mirror(ctx, repository.Path, repository.Source, repository.Branch)
}

func (s *snapshotsService) compare(
	ctx context.Context,
	repository entity.SnapshotRepository,
	state entity.RemoteState,
	pushed bool,
) entity.RemoteState {
	upstream := "refs/remotes/origin/" + state.Default

	tip, err := s.worktrees.Resolve(ctx, repository.Path, upstream)
	if err != nil {
		state.Failure = err.Error()

		return state
	}

	state.DefaultTip = tip

	diverged, err := s.worktrees.Divergence(ctx, repository.Path, "HEAD", upstream)
	if err != nil {
		state.Failure = err.Error()

		return state
	}

	state.Ahead, state.Behind = diverged.Ahead, diverged.Behind

	if pushed {
		own := "refs/remotes/origin/" + repository.Branch

		if state.BranchTip, err = s.worktrees.Resolve(ctx, repository.Path, own); err == nil {
			if moved, err := s.worktrees.Divergence(ctx, repository.Path, "HEAD", own); err == nil {
				state.Unmerged = moved.Behind
			}
		}
	}

	if state.Behind == 0 {
		return state
	}

	if state.Conflicts, err = s.worktrees.Conflicts(ctx, repository.Path, "HEAD", upstream); err != nil {
		state.Failure = err.Error()
	}

	return state
}
