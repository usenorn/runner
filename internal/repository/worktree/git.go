package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/gitcmd"
	"github.com/usenorn/runner/internal/repository"
)

const (
	authorName  = "Norn"
	authorEmail = "runner@norn.invalid"
)

type gitWorktree struct {
	cfg     config.Snapshot
	results config.Results
}

func New(cfg config.Snapshot, results config.Results) repository.Worktree {
	return &gitWorktree{cfg: cfg, results: results}
}

func (r *gitWorktree) Head(ctx context.Context, repository string) (string, error) {
	return r.Resolve(ctx, repository, "HEAD")
}

func (r *gitWorktree) Resolve(
	ctx context.Context,
	repository string,
	revisions ...string,
) (string, error) {
	for _, revision := range revisions {
		if revision == "" {
			continue
		}

		sha, err := r.run(ctx, repository, "rev-parse", "--verify", "--quiet", revision+"^{commit}")
		if err == nil && sha != "" {
			return sha, nil
		}
	}

	return "", fmt.Errorf("%w: %s", entity.ErrSnapshotBaseMissing, strings.Join(revisions, ", "))
}

func (r *gitWorktree) Fetch(ctx context.Context, repository, branch string) error {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.FetchTimeout)
	defer cancel()

	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)

	_, err := gitcmd.Run(ctx, repository, "fetch", "--no-tags", "--quiet", "origin", refspec)

	return err
}

func (r *gitWorktree) Add(ctx context.Context, repository, dest, sha string) error {
	_, err := r.run(ctx, repository, "worktree", "add", "--detach", "--quiet", dest, sha)

	return err
}

func (r *gitWorktree) Clone(ctx context.Context, repository, dest, sha string) error {
	if _, err := r.run(
		ctx, "", "clone", "--quiet", "--no-checkout", "--reference-if-able", repository,
		repository, dest,
	); err != nil {
		return err
	}

	_, err := r.run(ctx, dest, "checkout", "--detach", "--quiet", sha)

	return err
}

func (r *gitWorktree) Branch(ctx context.Context, dest, name string) error {
	_, err := r.run(ctx, dest, "switch", "--quiet", "-c", name)
	if err == nil {
		return nil
	}

	if !strings.Contains(err.Error(), "already exists") {
		return err
	}

	if _, err := r.run(ctx, dest, "switch", "--quiet", name); err != nil {
		if strings.Contains(err.Error(), "already used by worktree") {
			stale, pruneErr := r.staleWorktreePath(ctx, dest, name)
			if stale != "" && pruneErr == nil {
				if _, removeErr := r.run(ctx, dest, "worktree", "remove", "--force", stale); removeErr == nil {
					return r.Branch(ctx, dest, name)
				}
			}

			return fmt.Errorf("%w: %s", entity.ErrSnapshotWorktreeExists, name)
		}

		return err
	}

	return nil
}

func (r *gitWorktree) staleWorktreePath(ctx context.Context, repository, branch string) (string, error) {
	out, err := r.run(ctx, repository, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}

	var path string

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path = strings.TrimPrefix(line, "worktree ")
			continue
		}

		if strings.HasPrefix(line, "branch ") && strings.TrimPrefix(line, "branch ") == "refs/heads/"+branch {
			if _, statErr := os.Stat(path); statErr != nil {
				return path, nil
			}

			return "", nil
		}
	}

	return "", nil
}

func (r *gitWorktree) Prune(ctx context.Context, repository string) error {
	_, err := r.run(ctx, repository, "worktree", "prune")

	return err
}

func (r *gitWorktree) Submodules(ctx context.Context, dest string) error {
	_, err := r.run(ctx, dest, "submodule", "update", "--init", "--recursive", "--quiet")

	return err
}

func (r *gitWorktree) Changed(ctx context.Context, repository string) ([]string, error) {
	return r.paths(ctx, repository, "diff", "HEAD", "--name-only", "-z")
}

func (r *gitWorktree) Untracked(ctx context.Context, repository string) ([]string, error) {
	return r.paths(ctx, repository, "ls-files", "--others", "--exclude-standard", "-z")
}

func (r *gitWorktree) Diff(
	ctx context.Context,
	repository string,
	paths []string,
) ([]byte, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	args := append([]string{"diff", "HEAD", "--binary", "--no-color", "--"}, paths...)

	return r.raw(ctx, repository, args...)
}

func (r *gitWorktree) Apply(ctx context.Context, dest string, patch []byte) error {
	if len(patch) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, r.cfg.GitTimeout)
	defer cancel()

	command := gitcmd.Command(ctx, dest, "apply", "--index", "--binary", "--whitespace=nowarn", "-")
	command.Stdin = bytes.NewReader(patch)

	var complaint bytes.Buffer

	command.Stderr = &complaint

	if err := command.Run(); err != nil {
		return fmt.Errorf(
			"%w: %s", entity.ErrSnapshotDirtyConflict, tidy(complaint.String(), err),
		)
	}

	return nil
}

func (r *gitWorktree) Stage(ctx context.Context, dest string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	_, err := r.run(ctx, dest, append([]string{"add", "--"}, paths...)...)

	return err
}

func (r *gitWorktree) Commit(ctx context.Context, dest, message string) (string, error) {
	if _, err := r.run(ctx, dest,
		"-c", "user.name="+authorName,
		"-c", "user.email="+authorEmail,
		"commit", "--quiet", "--no-verify", "--no-gpg-sign", "-m", message,
	); err != nil {
		return "", err
	}

	return r.Head(ctx, dest)
}

func (r *gitWorktree) Remote(ctx context.Context, repository string) (string, error) {
	url, err := r.run(ctx, repository, "config", "--get", "remote.origin.url")
	if err != nil || strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("%w: %s", entity.ErrPushNowhere, repository)
	}

	return strings.TrimSpace(url), nil
}

func (r *gitWorktree) Commits(ctx context.Context, dest, base string) (int, error) {
	counted, err := r.run(ctx, dest, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return 0, err
	}

	count, err := strconv.Atoi(strings.TrimSpace(counted))
	if err != nil {
		return 0, fmt.Errorf("read how many commits %s has: %w", dest, err)
	}

	return count, nil
}

func (r *gitWorktree) Diffstat(
	ctx context.Context,
	dest, base string,
) (entity.Diffstat, error) {
	lines, err := gitcmd.Lines(ctx, dest, "diff", "--numstat", "--no-color", base+"..HEAD")
	if err != nil {
		return entity.Diffstat{}, err
	}

	stat := entity.Diffstat{}

	for _, line := range lines {
		columns := strings.SplitN(line, "\t", 3)
		if len(columns) != 3 {
			continue
		}

		stat.Files++
		stat.Additions += counted(columns[0])
		stat.Deletions += counted(columns[1])
	}

	return stat, nil
}

func (r *gitWorktree) Patch(ctx context.Context, dest, base string) ([]byte, error) {
	return r.raw(ctx, dest, "diff", "--binary", "--no-color", base+"..HEAD")
}

func (r *gitWorktree) Push(ctx context.Context, dest, url, branch string) error {
	ctx, cancel := context.WithTimeout(ctx, r.results.PushTimeout)
	defer cancel()

	_, err := gitcmd.Run(ctx, dest, "push", "--quiet", url, "HEAD:refs/heads/"+branch)

	return err
}

func counted(column string) int {
	value, err := strconv.Atoi(column)
	if err != nil {
		return 0
	}

	return value
}

func (r *gitWorktree) Remove(ctx context.Context, repository, dest string) error {
	_, removed := r.run(ctx, repository, "worktree", "remove", "--force", dest)

	if _, err := r.run(ctx, repository, "worktree", "prune"); err != nil {
		return err
	}

	return removed
}

func (r *gitWorktree) paths(
	ctx context.Context,
	repository string,
	args ...string,
) ([]string, error) {
	out, err := r.raw(ctx, repository, args...)
	if err != nil {
		return nil, err
	}

	found := make([]string, 0, bytes.Count(out, []byte{0}))

	for _, entry := range bytes.Split(out, []byte{0}) {
		if len(entry) > 0 {
			found = append(found, string(entry))
		}
	}

	return found, nil
}

func (r *gitWorktree) run(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.GitTimeout)
	defer cancel()

	return gitcmd.Run(ctx, dir, args...)
}

func (r *gitWorktree) raw(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.GitTimeout)
	defer cancel()

	command := gitcmd.Command(ctx, dir, args...)

	var complaint bytes.Buffer

	command.Stderr = &complaint

	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), tidy(complaint.String(), err))
	}

	return out, nil
}

func tidy(complaint string, err error) string {
	trimmed := strings.TrimSpace(complaint)
	if trimmed == "" {
		return err.Error()
	}

	return strings.ReplaceAll(trimmed, "\n", "; ")
}
