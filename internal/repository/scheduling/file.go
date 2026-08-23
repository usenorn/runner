package scheduling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
)

const version = 1

type stored struct {
	Version   int       `json:"version"`
	Paused    bool      `json:"paused"`
	ChangedAt time.Time `json:"changedAt"`
}

type fileScheduling struct {
	dir *statedir.Dir
	now func() time.Time
}

func New(dir *statedir.Dir) repository.Scheduling {
	return &fileScheduling{dir: dir, now: func() time.Time { return time.Now().UTC() }}
}

func (r *fileScheduling) Paused(_ context.Context) (bool, error) {
	raw, err := os.ReadFile(r.dir.Scheduling())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("read %s: %w", r.dir.Scheduling(), err)
	}

	var held stored

	if err := json.Unmarshal(raw, &held); err != nil || held.Version != version {
		return false, nil
	}

	return held.Paused, nil
}

func (r *fileScheduling) Pause(_ context.Context, paused bool) error {
	raw, err := json.MarshalIndent(stored{
		Version:   version,
		Paused:    paused,
		ChangedAt: r.now(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("write whether this machine is taking work: %w", err)
	}

	return statedir.WriteSecret(r.dir.Scheduling(), append(raw, '\n'))
}
