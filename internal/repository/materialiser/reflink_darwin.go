package materialiser

import "golang.org/x/sys/unix"

func reflink(from, to string) error {
	return unix.Clonefile(from, to, 0)
}
