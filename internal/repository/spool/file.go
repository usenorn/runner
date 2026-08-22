package spool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
)

const (
	suffix  = ".json"
	version = 1
)

type storedMessage struct {
	Version     int             `json:"version"`
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	ExecutionID string          `json:"execId,omitempty"`
	IssuedAt    time.Time       `json:"issuedAt"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

type fileSpool struct {
	dir string
}

func New(dir *statedir.Dir) repository.Spool {
	return &fileSpool{dir: dir.Spool()}
}

func (r *fileSpool) Append(_ context.Context, message channelv1.Message) error {
	raw, err := json.Marshal(storedMessage{
		Version:     version,
		ID:          message.ID,
		Type:        string(message.Type),
		ExecutionID: message.ExecutionID,
		IssuedAt:    message.IssuedAt.UTC(),
		Payload:     json.RawMessage(message.Payload),
	})
	if err != nil {
		return fmt.Errorf("encode the message %s: %w", message.ID, err)
	}

	return statedir.WriteSecret(r.path(message.ID), raw)
}

func (r *fileSpool) Head(_ context.Context, limit int) ([]channelv1.Message, error) {
	names, err := r.names()
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}

	held := make([]channelv1.Message, 0, len(names))

	for _, name := range names {
		message, err := r.read(name)
		if err != nil {
			return nil, err
		}

		if message.ID == "" {
			continue
		}

		held = append(held, message)
	}

	return held, nil
}

func (r *fileSpool) Acknowledge(_ context.Context, id string) error {
	if err := os.Remove(r.path(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("take %s out of the spool: %w", id, err)
	}

	return nil
}

func (r *fileSpool) Prune(_ context.Context, before time.Time, keep int) (int, error) {
	names, err := r.names()
	if err != nil {
		return 0, err
	}

	dropped := 0

	for index, name := range names {
		stale := keep > 0 && len(names)-index > keep

		if !stale {
			message, err := r.read(name)
			if err != nil {
				return dropped, err
			}

			stale = message.ID == "" || message.IssuedAt.Before(before)
		}

		if !stale {
			continue
		}

		if err := os.Remove(filepath.Join(r.dir, name)); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return dropped, fmt.Errorf("take %s out of the spool: %w", name, err)
		}

		dropped++
	}

	return dropped, nil
}

func (r *fileSpool) Count(_ context.Context) (int, error) {
	names, err := r.names()
	if err != nil {
		return 0, err
	}

	return len(names), nil
}

func (r *fileSpool) names() ([]string, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read the spool at %s: %w", r.dir, err)
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}

		names = append(names, entry.Name())
	}

	sort.Strings(names)

	return names, nil
}

func (r *fileSpool) read(name string) (channelv1.Message, error) {
	raw, err := os.ReadFile(filepath.Join(r.dir, name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return channelv1.Message{}, nil
		}

		return channelv1.Message{}, fmt.Errorf("read %s: %w", name, err)
	}

	var stored storedMessage

	if err := json.Unmarshal(raw, &stored); err != nil || stored.Version != version {
		return channelv1.Message{}, nil
	}

	return channelv1.Message{
		ID:          stored.ID,
		Type:        channelv1.MessageType(stored.Type),
		ExecutionID: stored.ExecutionID,
		IssuedAt:    stored.IssuedAt,
		Payload:     []byte(stored.Payload),
	}, nil
}

func (r *fileSpool) path(id string) string {
	return filepath.Join(r.dir, id+suffix)
}
