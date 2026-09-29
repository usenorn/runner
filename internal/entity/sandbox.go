package entity

import (
	"errors"
	"fmt"
)

const RuntimeAuto = "auto"

var (
	ErrRuntimeUnsupported = errors.New("this machine runs work as host processes or in docker")
	ErrRuntimeUnavailable = errors.New("the runtime this run asked for is not available on this machine")
)

type Sandbox struct {
	Run     string
	Runtime Runtime
}

func ChooseRuntime(asked, configured string) (Runtime, string, error) {
	if asked != "" && asked != RuntimeAuto {
		named := Runtime(asked)
		if !named.Valid() {
			return "", "", fmt.Errorf("%w, not in %s", ErrRuntimeUnsupported, asked)
		}

		return named, "the delegation asked for it", nil
	}

	if named := Runtime(configured); named.Valid() {
		return named, "this machine's configuration asks for it", nil
	}

	return RuntimeProcess, "nothing asked for anything else, so the work runs as host processes", nil
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
