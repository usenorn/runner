//go:build !unix

package proc

import (
	"errors"
	"os/exec"
)

var errUnsupported = errors.New("this machine cannot signal a process group")

func contain(command *exec.Cmd) {
	command.WaitDelay = SettleDelay
}

func stoppable(command *exec.Cmd) {
	contain(command)
}

func terminate(int) error {
	return errUnsupported
}

func kill(int) error {
	return errUnsupported
}

func alive(int) bool {
	return false
}
