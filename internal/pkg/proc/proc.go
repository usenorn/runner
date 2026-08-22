package proc

import (
	"os/exec"
	"time"
)

const SettleDelay = 5 * time.Second

func Contain(command *exec.Cmd) {
	contain(command)
}

func Stoppable(command *exec.Cmd) {
	stoppable(command)
}

func Terminate(pid int) error {
	return terminate(pid)
}

func Kill(pid int) error {
	return kill(pid)
}

func Alive(pid int) bool {
	return alive(pid)
}
