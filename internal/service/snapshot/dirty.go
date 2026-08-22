package snapshot

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/usenorn/runner/internal/entity"
)

const patchMode = 0o600

func (s *snapshotsService) carryLocalChanges(
	ctx context.Context,
	snapshot entity.Snapshot,
	rules []entity.IgnoreRule,
) (entity.Snapshot, error) {
	known := make(map[string]bool, len(snapshot.Repositories))

	for _, held := range snapshot.Repositories {
		known[held.RelPath] = true
	}

	for index, held := range snapshot.Repositories {
		patch, warnings, err := s.localChanges(ctx, snapshot, held, rules, known)

		snapshot.Warnings = append(snapshot.Warnings, warnings...)

		if err != nil {
			return snapshot, err
		}

		snapshot.Repositories[index].Local = patch
	}

	return snapshot, nil
}

func (s *snapshotsService) localChanges(
	ctx context.Context,
	snapshot entity.Snapshot,
	held entity.SnapshotRepository,
	rules []entity.IgnoreRule,
	known map[string]bool,
) (*entity.LocalPatch, []string, error) {
	warnings := []string{}

	head, err := s.worktrees.Head(ctx, held.Source)
	if err != nil {
		return nil, append(warnings, held.RelPath+
			" has no commit of its own, so there was no local work to carry across"), nil
	}

	own, err := s.settings.Ignores(ctx, held.Source)
	if err != nil {
		return nil, warnings, err
	}

	ignores := entity.NewIgnoreSet(rules, own)

	changed, err := s.worktrees.Changed(ctx, held.Source)
	if err != nil {
		return nil, warnings, err
	}

	untracked, skipped, err := s.untracked(ctx, held, ignores, known)
	if err != nil {
		return nil, warnings, err
	}

	warnings = append(warnings, skipped...)

	tracked := withoutSecrets(changed, ignores)

	if len(tracked) == 0 && len(untracked) == 0 {
		return nil, warnings, nil
	}

	patch, err := s.worktrees.Diff(ctx, held.Source, tracked)
	if err != nil {
		return nil, warnings, err
	}

	local := entity.LocalPatch{BaseSHA: head, Files: len(tracked) + len(untracked)}

	if len(patch) > 0 {
		local.PatchFile, err = s.recordPatch(snapshot, held, patch)
		if err != nil {
			return nil, warnings, err
		}
	}

	if err := s.worktrees.Apply(ctx, held.Path, patch); err != nil {
		return nil, warnings, fmt.Errorf("%s: %w", held.RelPath, err)
	}

	if _, err := s.materialiser.CopyPaths(ctx, held.Source, held.Path, untracked); err != nil {
		return nil, warnings, err
	}

	if err := s.worktrees.Stage(ctx, held.Path, untracked); err != nil {
		return nil, warnings, err
	}

	local.Commit, err = s.worktrees.Commit(ctx, held.Path, entity.LocalChangesMessage(head))
	if err != nil {
		return nil, warnings, err
	}

	return &local, warnings, nil
}

func (s *snapshotsService) untracked(
	ctx context.Context,
	held entity.SnapshotRepository,
	ignores entity.IgnoreSet,
	known map[string]bool,
) ([]string, []string, error) {
	found, err := s.worktrees.Untracked(ctx, held.Source)
	if err != nil {
		return nil, nil, err
	}

	warnings := []string{}
	kept := make([]string, 0, len(found))

	for _, relPath := range found {
		if trimmed, embedded := strings.CutSuffix(relPath, "/"); embedded {
			if known[path.Join(held.RelPath, trimmed)] {
				continue
			}

			warnings = append(warnings, path.Join(held.RelPath, trimmed)+
				" is a git repository of its own that norn was not told about, so its uncommitted "+
				"work was left behind")

			continue
		}

		if ignores.Decide(relPath, false) == entity.IgnoreKeep {
			kept = append(kept, relPath)
		}
	}

	return kept, warnings, nil
}

func (s *snapshotsService) recordPatch(
	snapshot entity.Snapshot,
	held entity.SnapshotRepository,
	patch []byte,
) (string, error) {
	dir := filepath.Join(snapshot.Run, entity.RunMetadataDir, entity.SnapshotPatchDir)

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}

	name := patchName(held) + ".patch"

	if err := os.WriteFile(filepath.Join(dir, name), patch, patchMode); err != nil {
		return "", fmt.Errorf("write %s: %w", filepath.Join(dir, name), err)
	}

	return filepath.Join(entity.SnapshotPatchDir, name), nil
}

// patchName falls back to the repository's own name for a repository at the root of the folder,
// because its relative path is "." and a patch called "..patch" is hidden from whoever looks.
func patchName(held entity.SnapshotRepository) string {
	if held.RelPath == entity.RepositoryRoot {
		return held.Name
	}

	return strings.ReplaceAll(held.RelPath, "/", "-")
}

func withoutSecrets(paths []string, ignores entity.IgnoreSet) []string {
	kept := make([]string, 0, len(paths))

	for _, relPath := range paths {
		if ignores.Decide(relPath, false) != entity.IgnoreDenied {
			kept = append(kept, relPath)
		}
	}

	return kept
}
