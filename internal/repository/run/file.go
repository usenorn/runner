package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
)

const (
	recordFile = "repositories.json"
	dirMode    = 0o700
	version    = 1
)

type storedPatch struct {
	BaseSHA   string `json:"baseSha"`
	Commit    string `json:"commit"`
	PatchFile string `json:"patchFile,omitempty"`
	Files     int    `json:"files"`
}

type storedRepository struct {
	Name    string       `json:"name"`
	RelPath string       `json:"relPath"`
	Kind    string       `json:"kind"`
	Source  string       `json:"source"`
	Path    string       `json:"path"`
	Mode    string       `json:"mode"`
	Base    string       `json:"base"`
	BaseSHA string       `json:"baseSha"`
	Branch  string       `json:"branch"`
	Local   *storedPatch `json:"localChanges,omitempty"`
}

type storedShared struct {
	RelPath string `json:"relPath"`
	Method  string `json:"method"`
	Size    int64  `json:"size"`
}

type stored struct {
	Version      int                `json:"version"`
	Name         string             `json:"name"`
	IssueKey     string             `json:"issueKey"`
	Attempt      int                `json:"attempt"`
	CodebaseID   uuid.UUID          `json:"codebaseId"`
	CodebaseRoot string             `json:"codebaseRoot"`
	Workspace    string             `json:"workspace"`
	Repositories []storedRepository `json:"repositories"`
	Shared       []storedShared     `json:"shared"`
	Bytes        int64              `json:"bytes"`
	Warnings     []string           `json:"warnings,omitempty"`
	TakenAt      time.Time          `json:"takenAt"`
	TookMillis   int64              `json:"tookMillis"`
}

type fileRun struct {
	dir *statedir.Dir
}

func New(dir *statedir.Dir) repository.Run {
	return &fileRun{dir: dir}
}

func (r *fileRun) Prepare(_ context.Context, name string) (string, error) {
	path := r.dir.Run(name)

	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%w: %s", entity.ErrSnapshotExists, path)
	}

	for _, child := range []string{
		filepath.Join(path, entity.SnapshotWorkspaceDir),
		filepath.Join(path, entity.SnapshotMetadataDir),
	} {
		if err := os.MkdirAll(child, dirMode); err != nil {
			return "", fmt.Errorf("create %s: %w", child, err)
		}
	}

	return path, nil
}

func (r *fileRun) Save(_ context.Context, snapshot entity.Snapshot) error {
	dir := filepath.Join(r.dir.Run(snapshot.Name), entity.SnapshotMetadataDir)

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	raw, err := json.MarshalIndent(storedOf(snapshot), "", "  ")
	if err != nil {
		return fmt.Errorf("write what %s holds: %w", snapshot.Name, err)
	}

	return statedir.WriteSecret(filepath.Join(dir, recordFile), append(raw, '\n'))
}

func (r *fileRun) Load(_ context.Context, name string) (entity.Snapshot, error) {
	return r.read(name)
}

func (r *fileRun) List(_ context.Context) ([]entity.Snapshot, error) {
	entries, err := os.ReadDir(r.dir.Runs())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read %s: %w", r.dir.Runs(), err)
	}

	snapshots := make([]entity.Snapshot, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		snapshot, err := r.read(entry.Name())
		if err != nil {
			if errors.Is(err, entity.ErrSnapshotMissing) {
				continue
			}

			return nil, err
		}

		snapshots = append(snapshots, snapshot)
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].TakenAt.Before(snapshots[j].TakenAt)
	})

	return snapshots, nil
}

func (r *fileRun) Remove(_ context.Context, name string) error {
	path := r.dir.Run(name)

	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%w: %s", entity.ErrSnapshotMissing, name)
	}

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}

	return nil
}

func (r *fileRun) read(name string) (entity.Snapshot, error) {
	path := filepath.Join(r.dir.Run(name), entity.SnapshotMetadataDir, recordFile)

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return entity.Snapshot{}, fmt.Errorf("%w: %s", entity.ErrSnapshotMissing, name)
		}

		return entity.Snapshot{}, fmt.Errorf("read %s: %w", path, err)
	}

	var held stored

	if err := json.Unmarshal(raw, &held); err != nil {
		return entity.Snapshot{}, fmt.Errorf("%s cannot be read: %w", path, err)
	}

	if held.Version != version || held.Name == "" {
		return entity.Snapshot{}, fmt.Errorf("%w: %s", entity.ErrSnapshotMissing, name)
	}

	return snapshotOf(held, r.dir.Run(name)), nil
}

func storedOf(snapshot entity.Snapshot) stored {
	held := stored{
		Version:      version,
		Name:         snapshot.Name,
		IssueKey:     snapshot.IssueKey,
		Attempt:      snapshot.Attempt,
		CodebaseID:   snapshot.CodebaseID,
		CodebaseRoot: snapshot.CodebaseRoot,
		Workspace:    snapshot.Workspace,
		Repositories: make([]storedRepository, 0, len(snapshot.Repositories)),
		Shared:       make([]storedShared, 0, len(snapshot.Shared)),
		Bytes:        snapshot.Bytes,
		Warnings:     snapshot.Warnings,
		TakenAt:      snapshot.TakenAt,
		TookMillis:   snapshot.Took.Milliseconds(),
	}

	for _, repository := range snapshot.Repositories {
		held.Repositories = append(held.Repositories, storedRepositoryOf(repository))
	}

	for _, shared := range snapshot.Shared {
		held.Shared = append(held.Shared, storedShared{
			RelPath: shared.RelPath,
			Method:  string(shared.Method),
			Size:    shared.Size,
		})
	}

	return held
}

func storedRepositoryOf(repository entity.SnapshotRepository) storedRepository {
	held := storedRepository{
		Name:    repository.Name,
		RelPath: repository.RelPath,
		Kind:    string(repository.Kind),
		Source:  repository.Source,
		Path:    repository.Path,
		Mode:    string(repository.Mode),
		Base:    string(repository.Base),
		BaseSHA: repository.BaseSHA,
		Branch:  repository.Branch,
	}

	if repository.Local != nil {
		held.Local = &storedPatch{
			BaseSHA:   repository.Local.BaseSHA,
			Commit:    repository.Local.Commit,
			PatchFile: repository.Local.PatchFile,
			Files:     repository.Local.Files,
		}
	}

	return held
}

func snapshotOf(held stored, run string) entity.Snapshot {
	snapshot := entity.Snapshot{
		Name:         held.Name,
		Run:          run,
		Workspace:    held.Workspace,
		IssueKey:     held.IssueKey,
		Attempt:      held.Attempt,
		CodebaseID:   held.CodebaseID,
		CodebaseRoot: held.CodebaseRoot,
		Repositories: make([]entity.SnapshotRepository, 0, len(held.Repositories)),
		Shared:       make([]entity.SharedFile, 0, len(held.Shared)),
		Bytes:        held.Bytes,
		Warnings:     held.Warnings,
		TakenAt:      held.TakenAt,
		Took:         time.Duration(held.TookMillis) * time.Millisecond,
	}

	for _, repository := range held.Repositories {
		snapshot.Repositories = append(snapshot.Repositories, repositoryOf(repository))
	}

	for _, shared := range held.Shared {
		snapshot.Shared = append(snapshot.Shared, entity.SharedFile{
			RelPath: shared.RelPath,
			Method:  entity.MaterialiseMethod(shared.Method),
			Size:    shared.Size,
		})
	}

	return snapshot
}

func repositoryOf(held storedRepository) entity.SnapshotRepository {
	repository := entity.SnapshotRepository{
		Name:    held.Name,
		RelPath: held.RelPath,
		Kind:    entity.RepositoryKind(held.Kind),
		Source:  held.Source,
		Path:    held.Path,
		Mode:    entity.GitMode(held.Mode),
		Base:    entity.BasePolicy(held.Base),
		BaseSHA: held.BaseSHA,
		Branch:  held.Branch,
	}

	if held.Local != nil {
		repository.Local = &entity.LocalPatch{
			BaseSHA:   held.Local.BaseSHA,
			Commit:    held.Local.Commit,
			PatchFile: held.Local.PatchFile,
			Files:     held.Local.Files,
		}
	}

	return repository
}
