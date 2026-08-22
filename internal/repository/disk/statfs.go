package disk

import (
	"context"
	"fmt"

	"github.com/usenorn/runner/internal/repository"
)

type statfsDisk struct{}

func New() repository.Disk {
	return &statfsDisk{}
}

func (r *statfsDisk) Free(_ context.Context, path string) (int64, error) {
	free, err := available(path)
	if err != nil {
		return 0, fmt.Errorf("read how much room is left on %s: %w", path, err)
	}

	return free, nil
}
