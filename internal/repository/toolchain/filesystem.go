package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

type filesystemToolchain struct{}

func New() repository.Toolchain {
	return &filesystemToolchain{}
}

func (r *filesystemToolchain) Manifests(
	_ context.Context,
	roots []entity.ManifestRoot,
) ([]entity.Manifest, error) {
	var manifests []entity.Manifest

	for _, root := range roots {
		found, err := scan(root.RelPath, root.Path, entity.ManifestScanDepth)
		if err != nil {
			return nil, err
		}

		manifests = append(manifests, found...)
	}

	return manifests, nil
}

func scan(relPath, path string, depth int) ([]entity.Manifest, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	here := entity.Manifest{RelPath: relPath}

	var manifests []entity.Manifest

	for _, entry := range entries {
		name := entry.Name()

		if !entry.IsDir() {
			if slices.Contains(entity.ManifestFiles(), name) {
				here.Files = append(here.Files, name)
			}

			continue
		}

		if depth == 0 || skipped(name) {
			continue
		}

		nested, err := scan(filepath.ToSlash(filepath.Join(relPath, name)), filepath.Join(path, name), depth-1)
		if err != nil {
			return nil, err
		}

		manifests = append(manifests, nested...)
	}

	if slices.Contains(here.Files, "package.json") {
		here.PackageManager = packageManager(filepath.Join(path, "package.json"))
	}

	if len(here.Files) == 0 {
		return manifests, nil
	}

	return append([]entity.Manifest{here}, manifests...), nil
}

func skipped(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor"
}

func packageManager(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var held struct {
		PackageManager string `json:"packageManager"`
	}

	if json.Unmarshal(raw, &held) != nil {
		return ""
	}

	return held.PackageManager
}

func (r *filesystemToolchain) Locate(name string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}

	return path, true
}
