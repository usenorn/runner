package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/gitcmd"
	"github.com/usenorn/runner/internal/repository"
)

const gitDirName = ".git"

type filesystemScanner struct {
	cfg config.Codebase
}

func New(cfg config.Codebase) repository.Scanner {
	return &filesystemScanner{cfg: cfg}
}

func (r *filesystemScanner) Scan(
	ctx context.Context,
	root string,
	depth int,
) (repository.ScannedFolder, error) {
	if !gitcmd.Installed() {
		return repository.ScannedFolder{}, entity.ErrGitMissing
	}

	resolved, err := resolve(root)
	if err != nil {
		return repository.ScannedFolder{}, err
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return repository.ScannedFolder{}, entity.ErrCodebaseRootMissing
	}

	if !info.IsDir() {
		return repository.ScannedFolder{}, entity.ErrCodebaseNotAFolder
	}

	folder := repository.ScannedFolder{Root: resolved, SharedFiles: sharedFiles(resolved)}

	walk := &walker{
		scanner: r,
		root:    resolved,
		depth:   clamp(depth),
		visited: map[string]struct{}{},
		folder:  &folder,
	}

	if err := walk.descend(ctx, resolved, 0); err != nil {
		return repository.ScannedFolder{}, err
	}

	return folder, nil
}

func clamp(depth int) int {
	switch {
	case depth < 1:
		return entity.ScanDepthDefault
	case depth > entity.ScanDepthMax:
		return entity.ScanDepthMax
	default:
		return depth
	}
}

func resolve(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}

	evaluated, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", entity.ErrCodebaseRootMissing
		}

		return "", fmt.Errorf("follow the links under %q: %w", path, err)
	}

	return evaluated, nil
}

func sharedFiles(root string) []string {
	found := make([]string, 0, len(entity.SharedFileNames()))

	for _, name := range entity.SharedFileNames() {
		if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
			found = append(found, name)
		}
	}

	return found
}

type walker struct {
	scanner *filesystemScanner
	root    string
	depth   int
	visited map[string]struct{}
	folder  *repository.ScannedFolder
}

func (w *walker) descend(ctx context.Context, dir string, level int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scan %q: %w", w.root, err)
	}

	if _, seen := w.visited[dir]; seen {
		return nil
	}

	w.visited[dir] = struct{}{}

	if holdsRepository(dir) {
		facts, ok := w.scanner.probe(ctx, dir)
		if ok {
			w.folder.Repositories = append(w.folder.Repositories, facts)
		}

		if ok && facts.Bare {
			return nil
		}
	}

	if level >= w.depth {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		w.warn("could not read " + w.relative(dir))

		return nil
	}

	for _, entry := range entries {
		child, ok := w.enterable(dir, entry)
		if !ok {
			continue
		}

		if err := w.descend(ctx, child, level+1); err != nil {
			return err
		}
	}

	return nil
}

func holdsRepository(dir string) bool {
	if _, err := os.Lstat(filepath.Join(dir, gitDirName)); err == nil {
		return true
	}

	for _, name := range []string{"HEAD", "objects", "refs"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			return false
		}
	}

	return true
}

func (w *walker) enterable(dir string, entry os.DirEntry) (string, bool) {
	name := entry.Name()

	if name == gitDirName || slices.Contains(entity.UninterestingDirNames(), name) {
		return "", false
	}

	path := filepath.Join(dir, name)

	if entry.Type()&os.ModeSymlink == 0 {
		return path, entry.IsDir()
	}

	target, err := resolve(path)
	if err != nil {
		return "", false
	}

	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return "", false
	}

	if !within(w.root, target) {
		w.warn(w.relative(path) + " links outside the folder and was not followed")

		return "", false
	}

	return target, true
}

func (w *walker) relative(dir string) string {
	relative, err := filepath.Rel(w.root, dir)
	if err != nil {
		return dir
	}

	return filepath.ToSlash(relative)
}

func (w *walker) warn(warning string) {
	w.folder.Warnings = append(w.folder.Warnings, warning)
}

func within(root, dir string) bool {
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}
