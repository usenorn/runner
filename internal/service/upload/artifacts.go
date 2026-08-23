package upload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

func (s *uploadsService) Publish(
	ctx context.Context,
	executionID string,
	artifact entity.Artifact,
) (entity.ArtifactReceipt, error) {
	if err := artifact.Valid(); err != nil {
		return entity.ArtifactReceipt{}, err
	}

	execution, err := s.runs.LoadTask(ctx, executionID)
	if err != nil {
		return entity.ArtifactReceipt{}, err
	}

	source, err := inside(
		filepath.Join(execution.Directory, entity.RunWorkspaceDir), artifact.Path,
	)
	if err != nil {
		return entity.ArtifactReceipt{}, err
	}

	held, err := os.Stat(source)
	if err != nil || !held.Mode().IsRegular() {
		return entity.ArtifactReceipt{}, fmt.Errorf("%w: %s", entity.ErrArtifactMissing, artifact.Path)
	}

	if held.Size() > s.cfg.MaxArtifactBytes {
		return entity.ArtifactReceipt{}, fmt.Errorf(
			"%w: %s is %s and norn takes %s",
			entity.ErrArtifactTooLarge, artifact.Path,
			entity.ByteSize(held.Size()), entity.ByteSize(s.cfg.MaxArtifactBytes),
		)
	}

	kept, err := s.keep(execution, source)
	if err != nil {
		return entity.ArtifactReceipt{}, err
	}

	token, err := s.sessions.Access(ctx)
	if err != nil {
		return entity.ArtifactReceipt{}, err
	}

	file, err := os.Open(kept)
	if err != nil {
		return entity.ArtifactReceipt{}, fmt.Errorf("read %s: %w", artifact.Path, err)
	}

	defer func() { _ = file.Close() }()

	return s.uploads.PublishArtifact(ctx, token, executionID, artifact.Label, file)
}

func (s *uploadsService) keep(execution entity.Execution, source string) (string, error) {
	dir := filepath.Join(execution.Directory, entity.RunArtifactsDir)

	if err := os.MkdirAll(dir, artifactDirMode); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}

	raw, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filepath.Base(source), err)
	}

	kept := filepath.Join(dir, filepath.Base(source))

	if err := statedir.WriteSecret(kept, raw); err != nil {
		return "", fmt.Errorf("keep %s with the run: %w", filepath.Base(source), err)
	}

	return kept, nil
}

func inside(workspace string, path string) (string, error) {
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", fmt.Errorf("%w: %s", entity.ErrArtifactMissing, path)
	}

	wanted := path
	if !filepath.IsAbs(wanted) {
		wanted = filepath.Join(root, wanted)
	}

	resolved, err := filepath.EvalSymlinks(wanted)
	if err != nil {
		return "", fmt.Errorf("%w: %s", entity.ErrArtifactMissing, path)
	}

	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"%w: %s sits outside %s", entity.ErrArtifactOutside, path, root,
		)
	}

	return resolved, nil
}
