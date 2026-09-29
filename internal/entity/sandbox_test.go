package entity_test

import (
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestARunsContainerHoldsItsFoldersAndTheGitItsBranchesLiveIn(t *testing.T) {
	execution := entity.Execution{ID: "exec-01ABC", Directory: "/state/runs/exec-01ABC", Runtime: "docker"}
	snapshot := entity.Snapshot{
		Workspace: "/state/runs/exec-01ABC/workspace",
		Repositories: []entity.SnapshotRepository{
			{Common: "/code/api/.git", Mode: entity.GitModeWorktree},
			{Common: "/code/web/.git", Mode: entity.GitModeClone},
			{Common: "/code/api/.git", Mode: entity.GitModeWorktree},
		},
	}

	spec := entity.SandboxSpecFor(execution, snapshot)

	mounted := map[string]bool{}
	for _, mount := range spec.Mounts {
		if _, twice := mounted[mount.Path]; twice {
			t.Fatalf("%s is mounted twice", mount.Path)
		}

		mounted[mount.Path] = mount.ReadOnly
	}

	for path, readOnly := range map[string]bool{
		"/state/runs/exec-01ABC/workspace": false,
		"/state/runs/exec-01ABC/home":      false,
		"/state/runs/exec-01ABC/tmp":       false,
		"/state/runs/exec-01ABC/metadata":  true,
		"/code/api/.git":                   false,
		"/code/web/.git":                   true,
	} {
		got, found := mounted[path]
		if !found || got != readOnly {
			t.Fatalf(
				"%s is mounted=%v read-only=%v, want read-only=%v. A worktree commits into the "+
					"repository it came from, and a clone only borrows its objects",
				path, found, got, readOnly,
			)
		}
	}

	if spec.Box.Runtime != entity.RuntimeDocker || spec.Workdir != snapshot.Workspace {
		t.Fatalf("the container is %+v, want docker working in the workspace", spec)
	}
}

func TestAnAgentInAContainerNeedsOnlyItsTokenFromTheHost(t *testing.T) {
	notOnHost := entity.DriverHealth{Installed: false, SignedIn: true}

	if err := notOnHost.FaultIn(entity.RuntimeDocker); err != nil {
		t.Fatalf(
			"a run in docker was faulted with %v because the agent is not on the host. The "+
				"agent runs from the image, so the host having it or not is beside the point",
			err,
		)
	}

	if err := notOnHost.FaultIn(entity.RuntimeProcess); err == nil {
		t.Fatal("a host-process run was let through with no agent installed on the host")
	}

	if err := (entity.DriverHealth{Installed: true}).FaultIn(entity.RuntimeDocker); err == nil {
		t.Fatal("a run in docker was let through with no token to sign the agent in")
	}
}
