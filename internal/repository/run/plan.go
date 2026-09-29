package run

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func (r *fileRun) LatestPlan(_ context.Context, name string) (string, error) {
	dir := entity.RunHomeOf(r.dir.Run(name)).Plans()

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", entity.ErrPlanMissing
	}

	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}

	var (
		newest  string
		written time.Time
	)

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != entity.PlanFileExt {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return "", fmt.Errorf("read %s: %w", entry.Name(), err)
		}

		if newest == "" || info.ModTime().After(written) {
			newest, written = entry.Name(), info.ModTime()
		}
	}

	if newest == "" {
		return "", entity.ErrPlanMissing
	}

	raw, err := os.ReadFile(filepath.Join(dir, newest))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", newest, err)
	}

	body := strings.TrimSpace(string(raw))
	if body == "" {
		return "", entity.ErrPlanMissing
	}

	return body, nil
}
