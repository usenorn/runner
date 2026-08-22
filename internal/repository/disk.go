package repository

import "context"

//go:generate go tool mockgen -source=disk.go -destination=disk/mock_disk.go -package=disk -mock_names=Disk=MockDisk

type Disk interface {
	Free(ctx context.Context, path string) (int64, error)
}
