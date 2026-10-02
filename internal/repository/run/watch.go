package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type storedWatch struct {
	Version   int           `json:"version"`
	Baselined bool          `json:"baselined"`
	Seen      []string      `json:"seen,omitempty"`
	Replies   []storedReply `json:"replies,omitempty"`
}

type storedReply struct {
	Thread string `json:"thread"`
	Body   string `json:"body"`
}

func (r *fileRun) watchPath(name string) string {
	return filepath.Join(r.dir.Run(name), entity.RunMetadataDir, entity.RunWatchFile)
}

func (r *fileRun) SaveWatch(_ context.Context, name string, watch entity.Watch) error {
	path := r.watchPath(name)

	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	replies := make([]storedReply, 0, len(watch.Replies))
	for _, reply := range watch.Replies {
		replies = append(replies, storedReply{Thread: reply.Thread, Body: reply.Body})
	}

	raw, err := json.MarshalIndent(storedWatch{
		Version:   version,
		Baselined: watch.Cursor.Baselined,
		Seen:      watch.Cursor.Seen,
		Replies:   replies,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("write what %s has seen of its pull requests: %w", name, err)
	}

	return statedir.WriteSecret(path, append(raw, '\n'))
}

func (r *fileRun) LoadWatch(_ context.Context, name string) (entity.Watch, error) {
	raw, err := os.ReadFile(r.watchPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return entity.Watch{}, nil
	}

	if err != nil {
		return entity.Watch{}, fmt.Errorf("read what %s has seen of its pull requests: %w", name, err)
	}

	var held storedWatch
	if err := json.Unmarshal(raw, &held); err != nil || held.Version != version {
		return entity.Watch{}, nil
	}

	watch := entity.Watch{Cursor: entity.WatchCursor{Baselined: held.Baselined, Seen: held.Seen}}
	for _, reply := range held.Replies {
		watch.Replies = append(watch.Replies, entity.PullRequestReply{Thread: reply.Thread, Body: reply.Body})
	}

	return watch, nil
}
