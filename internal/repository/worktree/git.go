package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/gitcmd"
	"github.com/usenorn/runner/internal/repository"
)

const (
	authorName  = "Norn"
	authorEmail = "runner@norn.invalid"

	mergeConflicted = 1
	notAncestor     = 1
)

type gitWorktree struct {
	cfg     config.Snapshot
	results config.Results
	locks   sync.Map
}

func New(cfg config.Snapshot, results config.Results) repository.Worktree {
	return &gitWorktree{cfg: cfg, results: results}
}

func (r *gitWorktree) hold(repository string) func() {
	held, _ := r.locks.LoadOrStore(filepath.Clean(repository), &sync.Mutex{})
	lock, _ := held.(*sync.Mutex)
	lock.Lock()

	return lock.Unlock
}

func (r *gitWorktree) Head(ctx context.Context, repository string) (string, error) {
	return r.Resolve(ctx, repository, "HEAD")
}

func (r *gitWorktree) CommonDir(ctx context.Context, repository string) (string, error) {
	return r.run(ctx, repository, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

func (r *gitWorktree) GitDir(ctx context.Context, dest string) (string, error) {
	return r.run(ctx, dest, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
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
	defer r.hold(repository)()

	ctx, cancel := context.WithTimeout(ctx, r.cfg.FetchTimeout)
	defer cancel()

	_, err := gitcmd.Run(ctx, repository, "fetch", "--no-tags", "--quiet", "origin", tracking(branch))

	return err
}

func (r *gitWorktree) FetchIfPresent(ctx context.Context, repository, branch string) (bool, error) {
	err := r.Fetch(ctx, repository, branch)
	if err == nil {
		return true, nil
	}

	if strings.Contains(err.Error(), "couldn't find remote ref") {
		return false, nil
	}

	return false, err
}

func (r *gitWorktree) Mirror(ctx context.Context, dest, source, branch string) error {
	_, err := r.run(ctx, dest, "fetch", "--no-tags", "--quiet", source,
		fmt.Sprintf("+refs/remotes/origin/%s:refs/remotes/origin/%s", branch, branch))

	return err
}

func tracking(branch string) string {
	return fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
}

func (r *gitWorktree) RemoteDefault(ctx context.Context, repository string) (string, error) {
	named, err := r.run(ctx, repository, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", fmt.Errorf("%w: %s", entity.ErrRemoteDefaultUnknown, repository)
	}

	return strings.TrimPrefix(strings.TrimSpace(named), "origin/"), nil
}

func (r *gitWorktree) Divergence(ctx context.Context, dest, ours, theirs string) (entity.Divergence, error) {
	sides, err := r.run(ctx, dest, "rev-list", "--left-right", "--count", ours+"..."+theirs)
	if err != nil {
		return entity.Divergence{}, err
	}

	ahead, behind, _ := strings.Cut(strings.TrimSpace(sides), "\t")

	return entity.Divergence{
		Ahead:  counted(strings.TrimSpace(ahead)),
		Behind: counted(strings.TrimSpace(behind)),
	}, nil
}

func (r *gitWorktree) Includes(ctx context.Context, dest, url, branch, tip string) (bool, error) {
	if _, err := r.run(ctx, dest, "cat-file", "-e", tip+"^{commit}"); err != nil {
		fetchCtx, cancel := context.WithTimeout(ctx, r.cfg.FetchTimeout)
		defer cancel()

		if _, err := gitcmd.Run(fetchCtx, dest, "fetch", "--no-tags", "--quiet", url, "refs/heads/"+branch); err != nil {
			return false, err
		}
	}

	_, err := r.run(ctx, dest, "merge-base", "--is-ancestor", tip, "HEAD")

	var exited *exec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == notAncestor {
		return false, nil
	}

	return err == nil, err
}

func (r *gitWorktree) Conflicts(ctx context.Context, dest, ours, theirs string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.GitTimeout)
	defer cancel()

	command := gitcmd.Command(
		ctx, dest, "merge-tree", "--write-tree", "--name-only", "--no-messages", ours, theirs,
	)

	var complaint bytes.Buffer

	command.Stderr = &complaint

	out, err := command.Output()

	if err == nil {
		return nil, nil
	}

	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != mergeConflicted {
		return nil, fmt.Errorf("git merge-tree %s %s: %s", ours, theirs, tidy(complaint.String(), err))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	conflicted := make([]string, 0, len(lines))

	for _, line := range lines[1:] {
		if line = strings.TrimSpace(line); line != "" && !slices.Contains(conflicted, line) {
			conflicted = append(conflicted, line)
		}
	}

	return conflicted, nil
}

func (r *gitWorktree) Add(ctx context.Context, repository, dest, sha string) error {
	defer r.hold(repository)()

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
		if heldElsewhere(err) {
			return fmt.Errorf("%w: %s", entity.ErrSnapshotWorktreeExists, name)
		}

		return err
	}

	return nil
}

func heldElsewhere(err error) bool {
	said := err.Error()

	return strings.Contains(said, "already used by worktree") || strings.Contains(said, "is already checked out at")
}

func (r *gitWorktree) Submodules(ctx context.Context, dest string) error {
	_, err := r.run(ctx, dest, "submodule", "update", "--init", "--recursive", "--quiet")

	return err
}

func (r *gitWorktree) Changed(ctx context.Context, repository string) ([]string, error) {
	return r.paths(ctx, repository, "diff", "HEAD", "--name-only", "--no-ext-diff", "-z")
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

	args := append(
		[]string{"diff", "HEAD", "--binary", "--no-color", "--no-ext-diff", "--no-textconv", "--"},
		paths...,
	)

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

func (r *gitWorktree) RemoteTip(ctx context.Context, url, branch string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.FetchTimeout)
	defer cancel()

	listed, err := gitcmd.Run(ctx, "", "ls-remote", "--heads", url, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}

	sha, _, _ := strings.Cut(listed, "\t")

	return strings.TrimSpace(sha), nil
}

func (r *gitWorktree) Commits(ctx context.Context, dest, base, head string) (int, error) {
	counted, err := r.run(ctx, dest, "rev-list", "--count", base+".."+head)
	if err != nil {
		return 0, err
	}

	count, err := strconv.Atoi(strings.TrimSpace(counted))
	if err != nil {
		return 0, fmt.Errorf("read how many commits %s has: %w", dest, err)
	}

	return count, nil
}

func (r *gitWorktree) History(
	ctx context.Context,
	dest, base, head string,
	limit int,
) ([]entity.Commit, error) {
	lines, err := gitcmd.Lines(
		ctx, dest, "log", "--no-color", "--no-ext-diff", "--no-textconv", "--format=%H%x00%s", "--max-count="+strconv.Itoa(limit),
		base+".."+head,
	)
	if err != nil {
		return nil, err
	}

	commits := make([]entity.Commit, 0, len(lines))

	for _, line := range lines {
		sha, subject, found := strings.Cut(line, "\x00")
		if !found {
			continue
		}

		commits = append(commits, entity.Commit{SHA: sha, Subject: subject})
	}

	return commits, nil
}

func (r *gitWorktree) Diffstat(
	ctx context.Context,
	dest, base, head string,
) (entity.Diffstat, error) {
	lines, err := gitcmd.Lines(
		ctx, dest, "diff", "--numstat", "--no-color", "--no-ext-diff", "--no-textconv", base+".."+head,
	)
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

func (r *gitWorktree) Patch(ctx context.Context, dest, base, head string) ([]byte, error) {
	return r.raw(ctx, dest, "diff", "--binary", "--no-color", "--no-ext-diff", "--no-textconv", base+".."+head)
}

func (r *gitWorktree) Push(ctx context.Context, dest, url string, push entity.Push) error {
	ctx, cancel := context.WithTimeout(ctx, r.results.PushTimeout)
	defer cancel()

	_, err := gitcmd.Run(ctx, dest, push.Arguments(url)...)

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
	defer r.hold(repository)()

	_, removed := r.run(ctx, repository, "worktree", "remove", "--force", dest)

	if _, err := r.run(ctx, repository, "worktree", "prune"); err != nil {
		return err
	}

	return removed
}

func (r *gitWorktree) Keep(
	ctx context.Context,
	repository, dest, branch, run string,
) (string, error) {
	defer r.hold(repository)()

	named := "refs/heads/" + branch
	if _, err := r.run(ctx, repository, "fetch", "--no-tags", "--quiet", dest, named+":"+named); err == nil {
		return named, nil
	}

	aside := entity.KeptRef(run, branch)
	if _, err := r.run(ctx, repository, "fetch", "--no-tags", "--quiet", dest, "+"+named+":"+aside); err != nil {
		return "", fmt.Errorf("keep %s from %s in %s: %w", branch, dest, repository, err)
	}

	return aside, nil
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
