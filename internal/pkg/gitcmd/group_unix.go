//go:build unix

package gitcmd

import (
	"os/exec"
	"syscall"
	"time"
)

const settleDelay = 5 * time.Second

func contain(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = settleDelay
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	}
}
