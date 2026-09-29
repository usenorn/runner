package entity

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

const (
	RuntimeAuto = "auto"

	RunToolsRoute = "/executions/{executionId}/mcp"
)

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

func RunToolsPath(executionID string) string {
	return strings.Replace(RunToolsRoute, "{executionId}", url.PathEscape(executionID), 1)
}

func SandboxSpecFor(execution Execution, snapshot Snapshot) SandboxSpec {
	home := RunHomeOf(execution.Directory)
	mounts := []Mount{
		{Path: snapshot.Workspace},
		{Path: home.Root},
		{Path: home.Tmp},
		{Path: execution.Metadata(), ReadOnly: true},
	}

	for _, repository := range snapshot.Repositories {
		if repository.Common == "" || slices.ContainsFunc(mounts, func(held Mount) bool {
			return held.Path == repository.Common
		}) {
			continue
		}

		mounts = append(mounts, Mount{Path: repository.Common, ReadOnly: repository.Mode == GitModeClone})
	}

	return SandboxSpec{Box: execution.Sandbox(), Workdir: snapshot.Workspace, Mounts: mounts}
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
