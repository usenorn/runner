//go:build unix

package proc

import (
	"errors"
	"os/exec"
	"syscall"
)

func contain(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = SettleDelay
}

func stoppable(command *exec.Cmd) {
	contain(command)

	command.Cancel = func() error {
		return terminate(command.Process.Pid)
	}
}

func terminate(pid int) error {
	return signal(pid, syscall.SIGTERM)
}

func kill(pid int) error {
	return signal(pid, syscall.SIGKILL)
}

func signal(pid int, sign syscall.Signal) error {
	if err := syscall.Kill(-pid, sign); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	return nil
}

func alive(pid int) bool {
	return syscall.Kill(-pid, 0) == nil
}
