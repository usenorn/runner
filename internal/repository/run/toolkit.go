package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type storedToolkit struct {
	Version      int                  `json:"version"`
	Instructions string               `json:"instructions,omitempty"`
	Skills       []storedToolkitSkill `json:"skills,omitempty"`
	Servers      []storedServer       `json:"servers,omitempty"`
}

type storedToolkitSkill struct {
	Name        string `json:"name"`
	ContentHash string `json:"contentHash"`
	DownloadURL string `json:"downloadUrl"`
}

type storedServer struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

func (r *fileRun) SaveToolkit(_ context.Context, name string, toolkit entity.Toolkit) error {
	dir := filepath.Join(r.dir.Run(name), entity.RunMetadataDir)

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	stored := storedToolkit{Version: version, Instructions: toolkit.Instructions}

	for _, skill := range toolkit.Skills {
		stored.Skills = append(stored.Skills, storedToolkitSkill(skill))
	}

	for _, server := range toolkit.Servers {
		stored.Servers = append(stored.Servers, storedServer(server))
	}

	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("write the toolkit for %s: %w", name, err)
	}

	return statedir.WriteSecret(filepath.Join(dir, entity.RunToolkitFile), append(raw, '\n'))
}

func (r *fileRun) LoadToolkit(_ context.Context, name string) (entity.Toolkit, error) {
	var stored storedToolkit

	path := filepath.Join(r.dir.Run(name), entity.RunMetadataDir, entity.RunToolkitFile)

	if err := readInto(path, &stored); err != nil {
		return entity.Toolkit{}, err
	}

	toolkit := entity.Toolkit{Instructions: stored.Instructions}

	for _, skill := range stored.Skills {
		toolkit.Skills = append(toolkit.Skills, entity.ToolkitSkill(skill))
	}

	for _, server := range stored.Servers {
		toolkit.Servers = append(toolkit.Servers, entity.ToolkitServer(server))
	}

	return toolkit, nil
}
