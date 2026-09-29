package entity

import "errors"

var (
	ErrRuntimeUnsupported = errors.New("this machine runs work as host processes or in docker")
	ErrRuntimeUnavailable = errors.New("the runtime this run asked for is not available on this machine")
)

type Sandbox struct {
	Run     string
	Runtime Runtime
}

func (e Execution) Sandbox() Sandbox {
	return Sandbox{Run: e.ID, Runtime: Runtime(e.Runtime)}
}

type Mount struct {
	Path     string
	ReadOnly bool
}

type SandboxSpec struct {
	Box     Sandbox
	Workdir string
	Mounts  []Mount
	Ports   []int
}
