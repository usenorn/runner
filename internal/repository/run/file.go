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
	dirMode  = 0o700
	fileMode = 0o600
	version  = 1
)

func children(path string) []string {
	return []string{
		filepath.Join(path, entity.RunWorkspaceDir),
		filepath.Join(path, entity.RunMetadataDir),
		filepath.Join(path, entity.RunLogsDir),
		filepath.Join(path, entity.RunArtifactsDir),
	}
}

type storedTask struct {
	Version      int       `json:"version"`
	ID           string    `json:"id"`
	Reference    string    `json:"reference"`
	IssueKey     string    `json:"issueKey"`
	Attempt      int       `json:"attempt"`
	WorkspaceID  string    `json:"workspaceId"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	Brief        string    `json:"brief,omitempty"`
	Tool         string    `json:"tool,omitempty"`
	Model        string    `json:"model,omitempty"`
	Runtime      string    `json:"runtime,omitempty"`
	BaseRef      string    `json:"baseRef,omitempty"`
	IncludeDirty bool      `json:"includeDirty,omitempty"`
	Profile      string    `json:"profile,omitempty"`
	State        string    `json:"state"`
	Lease        time.Time `json:"leaseExpiresAt,omitzero"`
	AcceptedAt   time.Time `json:"acceptedAt"`
	StartedAt    time.Time `json:"startedAt,omitzero"`
	SettledAt    time.Time `json:"settledAt,omitzero"`
	KeepUntil    time.Time `json:"keepUntil,omitzero"`
}

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

func (r *fileRun) Prepare(ctx context.Context, name string) (string, error) {
	path := r.dir.Run(name)

	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%w: %s", entity.ErrSnapshotExists, path)
	}

	return r.Open(ctx, name)
}

func (r *fileRun) Open(_ context.Context, name string) (string, error) {
	path := r.dir.Run(name)

	for _, child := range children(path) {
		if err := os.MkdirAll(child, dirMode); err != nil {
			return "", fmt.Errorf("create %s: %w", child, err)
		}
	}

	return path, nil
}

func (r *fileRun) Retire(_ context.Context, name string) error {
	path := r.dir.Run(name)

	leaving := []string{
		filepath.Join(path, entity.RunWorkspaceDir),
		filepath.Join(path, entity.RunArtifactsDir),
		filepath.Join(path, entity.RunMetadataDir, entity.RunMCPFile),
	}

	for _, child := range leaving {
		if err := os.RemoveAll(child); err != nil {
			return fmt.Errorf("remove %s: %w", child, err)
		}
	}

	return nil
}

func (r *fileRun) Prune(_ context.Context, name string) error {
	workspace := filepath.Join(r.dir.Run(name), entity.RunWorkspaceDir)

	if err := os.RemoveAll(workspace); err != nil {
		return fmt.Errorf("remove %s: %w", workspace, err)
	}

	return nil
}

func (r *fileRun) Save(_ context.Context, snapshot entity.Snapshot) error {
	dir := filepath.Join(r.dir.Run(snapshot.Name), entity.RunMetadataDir)

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	raw, err := json.MarshalIndent(storedOf(snapshot), "", "  ")
	if err != nil {
		return fmt.Errorf("write what %s holds: %w", snapshot.Name, err)
	}

	return statedir.WriteSecret(filepath.Join(dir, entity.RunRepositoryFile), append(raw, '\n'))
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

func (r *fileRun) SaveTask(_ context.Context, execution entity.Execution) error {
	dir := filepath.Join(r.dir.Run(execution.ID), entity.RunMetadataDir)

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	raw, err := json.MarshalIndent(storedTaskOf(execution), "", "  ")
	if err != nil {
		return fmt.Errorf("write what %s is for: %w", execution.ID, err)
	}

	return statedir.WriteSecret(
		filepath.Join(dir, entity.ExecutionTaskFile), append(raw, '\n'),
	)
}

func (r *fileRun) LoadTasks(_ context.Context) ([]entity.Execution, error) {
	entries, err := os.ReadDir(r.dir.Runs())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read %s: %w", r.dir.Runs(), err)
	}

	executions := make([]entity.Execution, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		execution, err := r.readTask(entry.Name())
		if err != nil {
			return nil, err
		}

		if execution.ID == "" {
			continue
		}

		executions = append(executions, execution)
	}

	sort.Slice(executions, func(i, j int) bool {
		return executions[i].AcceptedAt.Before(executions[j].AcceptedAt)
	})

	return executions, nil
}

func (r *fileRun) LoadTask(_ context.Context, name string) (entity.Execution, error) {
	execution, err := r.readTask(name)
	if err != nil {
		return entity.Execution{}, err
	}

	if execution.ID == "" {
		return entity.Execution{}, fmt.Errorf("%w: %s", entity.ErrExecutionUnknown, name)
	}

	return execution, nil
}

func (r *fileRun) readTask(name string) (entity.Execution, error) {
	path := filepath.Join(r.dir.Run(name), entity.RunMetadataDir, entity.ExecutionTaskFile)

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return entity.Execution{}, nil
		}

		return entity.Execution{}, fmt.Errorf("read %s: %w", path, err)
	}

	var held storedTask

	if err := json.Unmarshal(raw, &held); err != nil || held.Version != version {
		return entity.Execution{}, nil
	}

	return entity.Execution{
		ID:           held.ID,
		Reference:    held.Reference,
		IssueKey:     held.IssueKey,
		Attempt:      held.Attempt,
		WorkspaceID:  held.WorkspaceID,
		Title:        held.Title,
		Description:  held.Description,
		Brief:        held.Brief,
		Tool:         held.Tool,
		Model:        held.Model,
		Runtime:      held.Runtime,
		BaseRef:      held.BaseRef,
		IncludeDirty: held.IncludeDirty,
		Profile:      held.Profile,
		Directory:    r.dir.Run(name),
		State:        entity.ExecutionState(held.State),
		Lease:        held.Lease,
		AcceptedAt:   held.AcceptedAt,
		StartedAt:    held.StartedAt,
		SettledAt:    held.SettledAt,
		KeepUntil:    held.KeepUntil,
	}, nil
}

func storedTaskOf(execution entity.Execution) storedTask {
	return storedTask{
		Version:      version,
		ID:           execution.ID,
		Reference:    execution.Reference,
		IssueKey:     execution.IssueKey,
		Attempt:      execution.Attempt,
		WorkspaceID:  execution.WorkspaceID,
		Title:        execution.Title,
		Description:  execution.Description,
		Brief:        execution.Brief,
		Tool:         execution.Tool,
		Model:        execution.Model,
		Runtime:      execution.Runtime,
		BaseRef:      execution.BaseRef,
		IncludeDirty: execution.IncludeDirty,
		Profile:      execution.Profile,
		State:        string(execution.State),
		Lease:        execution.Lease,
		AcceptedAt:   execution.AcceptedAt,
		StartedAt:    execution.StartedAt,
		SettledAt:    execution.SettledAt,
		KeepUntil:    execution.KeepUntil,
	}
}

func (r *fileRun) read(name string) (entity.Snapshot, error) {
	path := filepath.Join(r.dir.Run(name), entity.RunMetadataDir, entity.RunRepositoryFile)

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

func (r *fileRun) Usage(_ context.Context) ([]entity.RunUsage, error) {
	entries, err := os.ReadDir(r.dir.Runs())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read %s: %w", r.dir.Runs(), err)
	}

	usage := make([]entity.RunUsage, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		held, err := r.usageOf(entry.Name())
		if err != nil {
			return nil, err
		}

		usage = append(usage, held)
	}

	return usage, nil
}

func (r *fileRun) usageOf(name string) (entity.RunUsage, error) {
	path := r.dir.Run(name)

	held := entity.RunUsage{Name: name, Workspace: kept(path)}

	execution, err := r.readTask(name)
	if err != nil {
		return entity.RunUsage{}, err
	}

	if execution.ID != "" {
		held.Finished = execution.Finished()
		held.Settled = execution.SettledAt
		held.KeepUntil = execution.KeepUntil
	}

	bytes, err := occupied(path)
	if err != nil {
		return entity.RunUsage{}, err
	}

	held.Bytes = bytes

	return held, nil
}

func kept(path string) bool {
	for _, child := range []string{entity.RunWorkspaceDir, entity.RunArtifactsDir} {
		if _, err := os.Stat(filepath.Join(path, child)); err == nil {
			return true
		}
	}

	return false
}

func occupied(path string) (int64, error) {
	var held int64

	err := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}

			return err
		}

		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}

			return err
		}

		if info.Mode().IsRegular() {
			held += info.Size()
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("measure %s: %w", path, err)
	}

	return held, nil
}
