package repository

import "context"

//go:generate go tool mockgen -source=worktree.go -destination=worktree/mock_worktree.go -package=worktree -mock_names=Worktree=MockWorktree

type Worktree interface {
	Head(ctx context.Context, repository string) (string, error)
	Resolve(ctx context.Context, repository string, revisions ...string) (string, error)
	Fetch(ctx context.Context, repository, branch string) error
	Add(ctx context.Context, repository, dest, sha string) error
	Clone(ctx context.Context, repository, dest, sha string) error
	Branch(ctx context.Context, dest, name string) error
	Submodules(ctx context.Context, dest string) error
	Changed(ctx context.Context, repository string) ([]string, error)
	Untracked(ctx context.Context, repository string) ([]string, error)
	Diff(ctx context.Context, repository string, paths []string) ([]byte, error)
	Apply(ctx context.Context, dest string, patch []byte) error
	Stage(ctx context.Context, dest string, paths []string) error
	Commit(ctx context.Context, dest, message string) (string, error)
	Remove(ctx context.Context, repository, dest string) error
}
