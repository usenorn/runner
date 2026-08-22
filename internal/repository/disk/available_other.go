//go:build !darwin && !linux

package disk

import "errors"

var errUnsupported = errors.New("this platform cannot be asked how much room is left")

func available(_ string) (int64, error) {
	return 0, errUnsupported
}
