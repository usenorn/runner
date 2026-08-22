package inventory

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
	inventoryFile = "inventory.json"
	dirMode       = 0o700
	version       = 1
)

type storedRemote struct {
	Hash     string `json:"hash,omitempty"`
	Host     string `json:"host,omitempty"`
	PathTail string `json:"pathTail,omitempty"`
}

type storedRepository struct {
	Name          string       `json:"name"`
	RelPath       string       `json:"relPath"`
	Kind          string       `json:"kind"`
	DefaultBranch string       `json:"defaultBranch,omitempty"`
	Remote        storedRemote `json:"remote,omitempty"`
	CommonDir     string       `json:"commonDir,omitempty"`
	Parent        string       `json:"parent,omitempty"`
}

type storedTool struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type storedInventory struct {
	Name         string             `json:"name"`
	RootPath     string             `json:"rootPath"`
	Repositories []storedRepository `json:"repositories"`
	SharedFiles  []string           `json:"sharedFiles"`
	Runtimes     []string           `json:"runtimes"`
	Tools        []storedTool       `json:"tools"`
	ScannedAt    time.Time          `json:"scannedAt"`
}

type stored struct {
	Version     int             `json:"version"`
	CodebaseID  uuid.UUID       `json:"codebaseId"`
	Name        string          `json:"name"`
	RootPath    string          `json:"rootPath"`
	Confirmed   storedInventory `json:"confirmed"`
	Reported    storedInventory `json:"reported"`
	ConfirmedAt time.Time       `json:"confirmedAt"`
	ReportedAt  time.Time       `json:"reportedAt"`
}

type fileInventory struct {
	dir *statedir.Dir
}

func New(dir *statedir.Dir) repository.Inventory {
	return &fileInventory{dir: dir}
}

func (r *fileInventory) List(_ context.Context) ([]entity.Codebase, error) {
	entries, err := os.ReadDir(r.dir.Codebases())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read %s: %w", r.dir.Codebases(), err)
	}

	codebases := make([]entity.Codebase, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		codebase, err := r.read(r.path(entry.Name()))
		if err != nil {
			if errors.Is(err, entity.ErrCodebaseNotConnected) {
				continue
			}

			return nil, err
		}

		codebases = append(codebases, codebase)
	}

	sort.Slice(codebases, func(i, j int) bool {
		return codebases[i].RootPath < codebases[j].RootPath
	})

	return codebases, nil
}

func (r *fileInventory) Load(ctx context.Context, root string) (entity.Codebase, error) {
	codebases, err := r.List(ctx)
	if err != nil {
		return entity.Codebase{}, err
	}

	for _, codebase := range codebases {
		if codebase.RootPath == root {
			return codebase, nil
		}
	}

	return entity.Codebase{}, entity.ErrCodebaseNotConnected
}

func (r *fileInventory) Save(_ context.Context, codebase entity.Codebase) error {
	if codebase.ID == uuid.Nil {
		return fmt.Errorf("%w: it has no id from norn", entity.ErrCodebaseNotConnected)
	}

	dir := r.dir.Codebase(codebase.ID.String())

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	raw, err := json.MarshalIndent(storedOf(codebase), "", "  ")
	if err != nil {
		return fmt.Errorf("write the inventory of %s: %w", codebase.RootPath, err)
	}

	return statedir.WriteSecret(filepath.Join(dir, inventoryFile), append(raw, '\n'))
}

func (r *fileInventory) Remove(_ context.Context, id uuid.UUID) error {
	if err := os.RemoveAll(r.dir.Codebase(id.String())); err != nil {
		return fmt.Errorf("forget the codebase at %s: %w", r.dir.Codebase(id.String()), err)
	}

	return nil
}

func (r *fileInventory) path(id string) string {
	return filepath.Join(r.dir.Codebase(id), inventoryFile)
}

func (r *fileInventory) read(path string) (entity.Codebase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return entity.Codebase{}, entity.ErrCodebaseNotConnected
		}

		return entity.Codebase{}, fmt.Errorf("read the inventory %s: %w", path, err)
	}

	var held stored

	if err := json.Unmarshal(raw, &held); err != nil {
		return entity.Codebase{}, fmt.Errorf("the inventory %s cannot be read: %w", path, err)
	}

	if held.CodebaseID == uuid.Nil || held.Version != version {
		return entity.Codebase{}, entity.ErrCodebaseNotConnected
	}

	return codebaseOf(held), nil
}

func storedOf(codebase entity.Codebase) stored {
	return stored{
		Version:     version,
		CodebaseID:  codebase.ID,
		Name:        codebase.Name,
		RootPath:    codebase.RootPath,
		Confirmed:   storedInventoryOf(codebase.Confirmed),
		Reported:    storedInventoryOf(codebase.Reported),
		ConfirmedAt: codebase.ConfirmedAt,
		ReportedAt:  codebase.ReportedAt,
	}
}

func storedInventoryOf(inventory entity.Inventory) storedInventory {
	repositories := make([]storedRepository, 0, len(inventory.Repositories))
	for _, repository := range inventory.Repositories {
		repositories = append(repositories, storedRepository{
			Name:          repository.Name,
			RelPath:       repository.RelPath,
			Kind:          string(repository.Kind),
			DefaultBranch: repository.DefaultBranch,
			Remote: storedRemote{
				Hash:     repository.Remote.Hash,
				Host:     repository.Remote.Host,
				PathTail: repository.Remote.PathTail,
			},
			CommonDir: repository.CommonDir,
			Parent:    repository.Parent,
		})
	}

	runtimes := make([]string, 0, len(inventory.Runtimes))
	for _, runtime := range inventory.Runtimes {
		runtimes = append(runtimes, string(runtime))
	}

	tools := make([]storedTool, 0, len(inventory.Tools))
	for _, tool := range inventory.Tools {
		tools = append(tools, storedTool{Name: tool.Name, Version: tool.Version})
	}

	return storedInventory{
		Name:         inventory.Name,
		RootPath:     inventory.RootPath,
		Repositories: repositories,
		SharedFiles:  inventory.SharedFiles,
		Runtimes:     runtimes,
		Tools:        tools,
		ScannedAt:    inventory.ScannedAt,
	}
}

func codebaseOf(held stored) entity.Codebase {
	return entity.Codebase{
		ID:          held.CodebaseID,
		Name:        held.Name,
		RootPath:    held.RootPath,
		Confirmed:   inventoryOf(held.Confirmed),
		Reported:    inventoryOf(held.Reported),
		ConfirmedAt: held.ConfirmedAt,
		ReportedAt:  held.ReportedAt,
	}
}

func inventoryOf(held storedInventory) entity.Inventory {
	repositories := make([]entity.Repository, 0, len(held.Repositories))
	for _, repository := range held.Repositories {
		repositories = append(repositories, entity.Repository{
			Name:          repository.Name,
			RelPath:       repository.RelPath,
			Kind:          entity.RepositoryKind(repository.Kind),
			DefaultBranch: repository.DefaultBranch,
			Remote: entity.RemoteFingerprint{
				Hash:     repository.Remote.Hash,
				Host:     repository.Remote.Host,
				PathTail: repository.Remote.PathTail,
			},
			CommonDir: repository.CommonDir,
			Parent:    repository.Parent,
		})
	}

	runtimes := make([]entity.Runtime, 0, len(held.Runtimes))
	for _, runtime := range held.Runtimes {
		runtimes = append(runtimes, entity.Runtime(runtime))
	}

	tools := make([]entity.Tool, 0, len(held.Tools))
	for _, tool := range held.Tools {
		tools = append(tools, entity.Tool{Name: tool.Name, Version: tool.Version})
	}

	return entity.Inventory{
		Name:         held.Name,
		RootPath:     held.RootPath,
		Repositories: repositories,
		SharedFiles:  held.SharedFiles,
		Runtimes:     runtimes,
		Tools:        tools,
		ScannedAt:    held.ScannedAt,
	}
}
