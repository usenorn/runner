package materialiser

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

const (
	dirMode  = 0o755
	fileMode = 0o644
)

type fileMaterialiser struct{}

func New() repository.Materialiser {
	return &fileMaterialiser{}
}

type run struct {
	from   string
	to     string
	budget int64
	made   []string
	result repository.Materialised
}

func (r *fileMaterialiser) Copy(
	ctx context.Context,
	from, to string,
	skip repository.SkipFunc,
	budget int64,
) (repository.Materialised, error) {
	pass := &run{from: from, to: to, budget: budget}

	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		relPath, err := filepath.Rel(from, path)
		if err != nil {
			return fmt.Errorf("place %s inside the snapshot: %w", path, err)
		}

		if relPath == "." {
			return os.MkdirAll(to, dirMode)
		}

		relPath = filepath.ToSlash(relPath)

		if skip != nil && skip(relPath, entry.IsDir()) {
			if entry.IsDir() {
				return fs.SkipDir
			}

			return nil
		}

		return pass.place(relPath, path, entry)
	})
	if err != nil {
		return pass.result, err
	}

	pass.prune()

	return pass.result, nil
}

func (r *fileMaterialiser) CopyPaths(
	ctx context.Context,
	from, to string,
	relPaths []string,
) (repository.Materialised, error) {
	pass := &run{from: from, to: to, budget: 0}

	for _, relPath := range relPaths {
		if ctx.Err() != nil {
			return pass.result, ctx.Err()
		}

		path := filepath.Join(from, filepath.FromSlash(relPath))

		info, err := os.Lstat(path)
		if err != nil {
			pass.warn(relPath + " went away while it was being copied")

			continue
		}

		if info.IsDir() {
			pass.warn(relPath + " is a folder of its own and was not carried across")

			continue
		}

		if err := pass.place(relPath, path, fs.FileInfoToDirEntry(info)); err != nil {
			return pass.result, err
		}
	}

	return pass.result, nil
}

func (r *fileMaterialiser) Remove(_ context.Context, dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}

	return nil
}

func (p *run) place(relPath, path string, entry fs.DirEntry) error {
	dest := filepath.Join(p.to, filepath.FromSlash(relPath))

	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	switch {
	case entry.IsDir():
		return p.directory(dest, info)
	case entry.Type()&fs.ModeSymlink != 0:
		return p.link(relPath, path, dest)
	case entry.Type().IsRegular():
		return p.file(relPath, path, dest, info)
	default:
		p.warn(relPath + " is not a file, a folder or a link, so it was left behind")

		return nil
	}
}

func (p *run) directory(dest string, info fs.FileInfo) error {
	if err := os.MkdirAll(dest, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}

	p.made = append(p.made, dest)

	if err := os.Chmod(dest, info.Mode().Perm()); err != nil {
		return fmt.Errorf("set the permissions of %s: %w", dest, err)
	}

	return nil
}

func (p *run) link(relPath, path, dest string) error {
	target, err := os.Readlink(path)
	if err != nil {
		return fmt.Errorf("read the link %s: %w", path, err)
	}

	if !p.reaches(path, target) {
		p.warn(relPath + " links outside the folder and was not carried across")

		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dest), err)
	}

	if err := os.Symlink(target, dest); err != nil {
		return fmt.Errorf("recreate the link %s: %w", dest, err)
	}

	p.result.Files = append(p.result.Files, entity.SharedFile{
		RelPath: relPath,
		Method:  entity.MaterialiseLink,
	})

	return nil
}

func (p *run) reaches(path, target string) bool {
	if filepath.IsAbs(target) {
		return false
	}

	resolved := filepath.Clean(filepath.Join(filepath.Dir(path), target))

	return resolved == p.from || strings.HasPrefix(resolved, p.from+string(filepath.Separator))
}

func (p *run) file(relPath, path, dest string, info fs.FileInfo) error {
	if p.budget > 0 && p.result.Bytes+info.Size() > p.budget {
		return fmt.Errorf(
			"%w: %s alone takes it past %d bytes. Exclude what an execution does not need with a "+
				"%s file",
			entity.ErrSnapshotTooLarge, relPath, p.budget, entity.IgnoreFileName,
		)
	}

	if err := os.MkdirAll(filepath.Dir(dest), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dest), err)
	}

	method := entity.MaterialiseReflink

	if err := reflink(path, dest); err != nil {
		method = entity.MaterialiseCopy

		if err := copyFile(path, dest, info.Mode().Perm()); err != nil {
			return err
		}
	}

	if err := os.Chmod(dest, info.Mode().Perm()); err != nil {
		return fmt.Errorf("set the permissions of %s: %w", dest, err)
	}

	p.result.Bytes += info.Size()
	p.result.Files = append(p.result.Files, entity.SharedFile{
		RelPath: relPath,
		Method:  method,
		Size:    info.Size(),
	})

	return nil
}

func (p *run) prune() {
	for index := len(p.made) - 1; index >= 0; index-- {
		_ = os.Remove(p.made[index])
	}
}

func (p *run) warn(warning string) {
	p.result.Warnings = append(p.result.Warnings, warning)
}

func copyFile(path, dest string, mode fs.FileMode) error {
	source, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	defer func() { _ = source.Close() }()

	if mode == 0 {
		mode = fileMode
	}

	target, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}

	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		_ = os.Remove(dest)

		return fmt.Errorf("copy %s into the snapshot: %w", path, err)
	}

	if err := target.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dest, err)
	}

	return nil
}
