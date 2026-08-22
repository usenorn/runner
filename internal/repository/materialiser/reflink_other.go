//go:build !darwin && !linux

package materialiser

import "errors"

var errNoReflink = errors.New("this filesystem cannot clone a file")

func reflink(_, _ string) error {
	return errNoReflink
}
