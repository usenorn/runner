//go:build !unix

package gitcmd

import "os/exec"

func contain(command *exec.Cmd) {
	_ = command
}
