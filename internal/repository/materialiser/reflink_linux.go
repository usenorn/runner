package materialiser

import (
	"os"

	"golang.org/x/sys/unix"
)

func reflink(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}

	defer func() { _ = source.Close() }()

	info, err := source.Stat()
	if err != nil {
		return err
	}

	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}

	if err := unix.IoctlFileClone(int(target.Fd()), int(source.Fd())); err != nil {
		_ = target.Close()
		_ = os.Remove(to)

		return err
	}

	return target.Close()
}
