package servicelog

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
)

const (
	dirMode  = 0o700
	fileMode = 0o600

	window = 1 << 20
)

type fileServiceLog struct {
	dir *statedir.Dir
}

func New(dir *statedir.Dir) repository.ServiceLog {
	return &fileServiceLog{dir: dir}
}

func (r *fileServiceLog) Open(
	_ context.Context,
	run string,
	name string,
) (io.WriteCloser, error) {
	path := r.path(run, name)

	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return nil, fmt.Errorf("make room for %s's log: %w", name, err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return nil, fmt.Errorf("open %s's log: %w", name, err)
	}

	return file, nil
}

func (r *fileServiceLog) Tail(
	_ context.Context,
	run string,
	name string,
	lines int,
) ([]string, error) {
	file, err := os.Open(r.path(run, name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", entity.ErrServiceUnknown, name)
		}

		return nil, fmt.Errorf("read %s's log: %w", name, err)
	}
	defer func() { _ = file.Close() }()

	held, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("measure %s's log: %w", name, err)
	}

	partial := held.Size() > window

	if partial {
		if _, err := file.Seek(held.Size()-window, io.SeekStart); err != nil {
			return nil, fmt.Errorf("read the end of %s's log: %w", name, err)
		}
	}

	return read(file, lines, partial)
}

func read(file *os.File, lines int, partial bool) ([]string, error) {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), entity.ServiceTailLine)

	kept := []string{}

	for scanner.Scan() {
		if partial {
			partial = false

			continue
		}

		kept = append(kept, scanner.Text())

		if lines > 0 && len(kept) > lines {
			kept = kept[1:]
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read a service log: %w", err)
	}

	return kept, nil
}

func (r *fileServiceLog) path(run string, name string) string {
	return filepath.Join(
		r.dir.Run(run), entity.RunLogsDir, entity.RunServiceLogsDir, entity.ServiceLogFile(name),
	)
}
