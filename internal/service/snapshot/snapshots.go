package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/gitcmd"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

const dirMode = 0o700

type snapshotsService struct {
	worktrees    repository.Worktree
	materialiser repository.Materialiser
	settings     repository.Settings
	inventories  repository.Inventory
	runs         repository.Run
	cfg          config.Snapshot
	now          func() time.Time
}

func New(
	worktrees repository.Worktree,
	materialiser repository.Materialiser,
	settings repository.Settings,
	inventories repository.Inventory,
	runs repository.Run,
	cfg config.Snapshot,
) service.Snapshots {
	return &snapshotsService{
		worktrees:    worktrees,
		materialiser: materialiser,
		settings:     settings,
		inventories:  inventories,
		runs:         runs,
		cfg:          cfg,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (s *snapshotsService) List(ctx context.Context) ([]entity.Snapshot, error) {
	return s.runs.List(ctx)
}

func (s *snapshotsService) Take(
	ctx context.Context,
	request service.TakeRequest,
) (entity.Snapshot, error) {
	if !gitcmd.Installed() {
		return entity.Snapshot{}, entity.ErrGitMissing
	}

	if strings.TrimSpace(request.IssueKey) == "" {
		return entity.Snapshot{}, entity.ErrSnapshotIssueKeyEmpty
	}

	codebase, err := s.codebaseFor(ctx, request.Path)
	if err != nil {
		return entity.Snapshot{}, err
	}

	listed := codebase.Confirmed.Listed()
	if len(listed) == 0 {
		return entity.Snapshot{}, entity.ErrSnapshotEmpty
	}

	policy, err := s.policy(ctx, codebase.RootPath, request)
	if err != nil {
		return entity.Snapshot{}, err
	}

	snapshot := entity.Snapshot{
		Name:         nameFor(request),
		IssueKey:     request.IssueKey,
		Branch:       request.Branch,
		Attempt:      max(request.Attempt, 1),
		CodebaseID:   codebase.ID,
		CodebaseRoot: codebase.RootPath,
		TakenAt:      s.now(),
	}

	if codebase.Drifted() {
		snapshot.Warnings = append(snapshot.Warnings, fmt.Sprintf(
			"this folder has drifted since it was confirmed, so the snapshot holds the %d "+
				"repositories that were confirmed; run 'norn runner inspect --confirm' to accept "+
				"it as it now stands",
			len(listed),
		))
	}

	run, err := s.open(ctx, request, snapshot.Name)
	if err != nil {
		return entity.Snapshot{}, err
	}

	snapshot.Run = run
	snapshot.Workspace = filepath.Join(run, entity.RunWorkspaceDir)

	taken, err := s.fill(ctx, snapshot, codebase, listed, policy, request.Branches)
	if err != nil {
		s.tear(ctx, snapshot.Name, taken.Repositories, request.Run == "")

		return entity.Snapshot{}, err
	}

	taken.Took = s.now().Sub(taken.TakenAt)

	if err := s.runs.Save(ctx, taken); err != nil {
		s.tear(ctx, snapshot.Name, taken.Repositories, request.Run == "")

		return entity.Snapshot{}, err
	}

	return taken, nil
}

func nameFor(request service.TakeRequest) string {
	if request.Run != "" {
		return request.Run
	}

	return entity.RunNameFor(request.IssueKey, request.Attempt)
}

func (s *snapshotsService) open(
	ctx context.Context,
	request service.TakeRequest,
	name string,
) (string, error) {
	if request.Run != "" {
		return s.runs.Open(ctx, name)
	}

	return s.runs.Prepare(ctx, name)
}

func (s *snapshotsService) fill(
	ctx context.Context,
	snapshot entity.Snapshot,
	codebase entity.Codebase,
	listed []entity.Repository,
	policy entity.SnapshotPolicy,
	branches map[string]string,
) (entity.Snapshot, error) {
	rules, err := s.settings.Ignores(ctx, codebase.RootPath)
	if err != nil {
		return snapshot, err
	}

	sort.Slice(listed, func(i, j int) bool { return listed[i].RelPath < listed[j].RelPath })

	for _, held := range listed {
		checked, warnings, err := s.checkout(
			ctx, snapshot, codebase.RootPath, held, policy, branches[held.Name],
		)

		snapshot.Warnings = append(snapshot.Warnings, warnings...)

		if checked.Path != "" {
			snapshot.Repositories = append(snapshot.Repositories, checked)
		}

		if err != nil {
			return snapshot, err
		}
	}

	shared, err := s.materialiser.Copy(
		ctx,
		codebase.RootPath,
		snapshot.Workspace,
		s.skipping(codebase.Confirmed, entity.NewIgnoreSet(rules)),
		s.cfg.MaxSharedBytes,
	)

	snapshot.Shared = shared.Files
	snapshot.Bytes = shared.Bytes
	snapshot.Warnings = append(snapshot.Warnings, shared.Warnings...)

	if err != nil {
		return snapshot, err
	}

	if policy.LocalChanges != entity.LocalChangesInclude {
		return snapshot, nil
	}

	return s.carryLocalChanges(ctx, snapshot, rules)
}

func (s *snapshotsService) checkout(
	ctx context.Context,
	snapshot entity.Snapshot,
	root string,
	held entity.Repository,
	policy entity.SnapshotPolicy,
	branch string,
) (entity.SnapshotRepository, []string, error) {
	source := filepath.Join(root, filepath.FromSlash(held.RelPath))
	path := filepath.Join(snapshot.Workspace, filepath.FromSlash(held.RelPath))

	base, warnings, err := s.base(ctx, source, held, policy)
	if err != nil {
		return entity.SnapshotRepository{}, warnings, err
	}

	if policy.GitMode == entity.GitModeClone {
		err = s.worktrees.Clone(ctx, source, path, base)
	} else {
		err = s.worktrees.Add(ctx, source, path, base)
	}

	if err != nil {
		return entity.SnapshotRepository{}, warnings, err
	}

	checked := entity.SnapshotRepository{
		Name:    held.Name,
		RelPath: held.RelPath,
		Kind:    held.Kind,
		Source:  source,
		Path:    path,
		Mode:    policy.GitMode,
		Base:    policy.Base,
		BaseSHA: base,
		Branch:  branchFor(branch, snapshot, held),
	}

	if err := s.worktrees.Branch(ctx, checked.Path, checked.Branch); err != nil {
		return checked, warnings, err
	}

	if err := s.worktrees.Submodules(ctx, checked.Path); err != nil {
		warnings = append(warnings, fmt.Sprintf(
			"%s has submodules that could not be filled in, so those folders are empty: %s",
			held.RelPath, err,
		))
	}

	return checked, warnings, nil
}

func (s *snapshotsService) base(
	ctx context.Context,
	source string,
	held entity.Repository,
	policy entity.SnapshotPolicy,
) (string, []string, error) {
	warnings := []string{}

	if policy.Base == entity.BaseHead || held.DefaultBranch == "" {
		if policy.Base != entity.BaseHead {
			warnings = append(warnings, held.RelPath+
				" names no default branch, so the snapshot starts from the commit it is on")
		}

		sha, err := s.worktrees.Head(ctx, source)

		return sha, warnings, err
	}

	if policy.Fetch && held.Remote.Known() {
		if err := s.worktrees.Fetch(ctx, source, held.DefaultBranch); err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"%s could not be fetched, so the snapshot starts from what this machine already "+
					"had: %s",
				held.RelPath, err,
			))
		}
	}

	sha, err := s.worktrees.Resolve(
		ctx, source, "refs/remotes/origin/"+held.DefaultBranch, held.DefaultBranch, "HEAD",
	)

	return sha, warnings, err
}

func (s *snapshotsService) skipping(
	inventory entity.Inventory,
	ignores entity.IgnoreSet,
) repository.SkipFunc {
	held := make(map[string]bool, len(inventory.Repositories))

	for _, repository := range inventory.Repositories {
		held[repository.RelPath] = true
	}

	// A repository at the root of the folder is the whole of it, and the checkout has already put
	// it in the workspace. The walk never offers the root itself to this, so nothing else would be
	// skipped: every file that repository holds would be copied over the checkout, and its .git —
	// a file in a worktree, not a folder — would be copied over as a folder.
	if held[entity.RepositoryRoot] {
		return func(string, bool) bool { return true }
	}

	return func(relPath string, isDir bool) bool {
		if held[relPath] {
			return true
		}

		return ignores.Decide(relPath, isDir) != entity.IgnoreKeep
	}
}

func (s *snapshotsService) policy(
	ctx context.Context,
	root string,
	asked service.TakeRequest,
) (entity.SnapshotPolicy, error) {
	policy := entity.SnapshotPolicy{
		GitMode:      entity.GitMode(s.cfg.GitMode),
		Base:         entity.BasePolicy(s.cfg.Base),
		LocalChanges: entity.LocalChanges(s.cfg.LocalChanges),
		Fetch:        s.cfg.Fetch,
	}

	held, err := s.settings.Load(ctx, root)
	if err != nil {
		return policy, err
	}

	if mode := entity.GitMode(held.GitMode); mode.Valid() {
		policy.GitMode = mode
	}

	if base := entity.BasePolicy(held.Base); base.Valid() {
		policy.Base = base
	}

	if changes := entity.LocalChanges(held.LocalChanges); changes.Valid() {
		policy.LocalChanges = changes
	}

	if held.Fetch != nil {
		policy.Fetch = *held.Fetch
	}

	if asked.LocalChanges.Valid() {
		policy.LocalChanges = asked.LocalChanges
	}

	if asked.Base.Valid() {
		policy.Base = asked.Base
	}

	return policy, nil
}

func (s *snapshotsService) codebaseFor(
	ctx context.Context,
	path string,
) (entity.Codebase, error) {
	wanted, err := resolve(path)
	if err != nil {
		return entity.Codebase{}, err
	}

	codebases, err := s.inventories.List(ctx)
	if err != nil {
		return entity.Codebase{}, err
	}

	for _, codebase := range codebases {
		if wanted == codebase.RootPath || under(codebase.RootPath, wanted) {
			return codebase, nil
		}
	}

	return entity.Codebase{}, entity.ErrCodebaseNotConnected
}

func branchFor(reused string, snapshot entity.Snapshot, held entity.Repository) string {
	if reused != "" {
		return reused
	}

	if snapshot.Branch != "" {
		return snapshot.Branch
	}

	return entity.BranchFor(snapshot.IssueKey, held.Name, snapshot.Attempt)
}

func (s *snapshotsService) Release(ctx context.Context, name string) error {
	snapshot, err := s.runs.Load(ctx, name)
	if err != nil && !errors.Is(err, entity.ErrSnapshotMissing) {
		return err
	}

	if err := s.release(ctx, snapshot.Repositories); err != nil {
		return err
	}

	return s.runs.Prune(ctx, name)
}

func (s *snapshotsService) Discard(ctx context.Context, name string) error {
	if err := s.Release(ctx, name); err != nil {
		return err
	}

	return s.runs.Remove(ctx, name)
}

func (s *snapshotsService) PruneWorktrees(ctx context.Context) error {
	codebases, err := s.inventories.List(ctx)
	if err != nil {
		return err
	}

	for _, codebase := range codebases {
		if err := s.worktrees.Prune(ctx, codebase.RootPath); err != nil {
			return err
		}
	}

	return nil
}

func (s *snapshotsService) release(
	ctx context.Context,
	repositories []entity.SnapshotRepository,
) error {
	for index := len(repositories) - 1; index >= 0; index-- {
		held := repositories[index]

		if held.Mode != entity.GitModeWorktree {
			continue
		}

		if err := s.worktrees.Remove(ctx, held.Source, held.Path); err != nil {
			return fmt.Errorf(
				"take the worktree of %s back out of %s: %w", held.RelPath, held.Source, err,
			)
		}
	}

	return nil
}

func (s *snapshotsService) tear(
	ctx context.Context,
	name string,
	repositories []entity.SnapshotRepository,
	made bool,
) {
	ctx = context.WithoutCancel(ctx)

	_ = s.release(ctx, repositories)

	if made {
		_ = s.runs.Remove(ctx, name)

		return
	}

	_ = s.runs.Prune(ctx, name)
}

func resolve(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}

	evaluated, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", entity.ErrCodebaseRootMissing
		}

		return "", fmt.Errorf("follow the links under %q: %w", path, err)
	}

	return evaluated, nil
}

func under(parent, child string) bool {
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}
