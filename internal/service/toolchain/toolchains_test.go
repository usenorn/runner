package toolchain_test

import (
	"context"
	"io"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	forgerepo "github.com/usenorn/runner/internal/repository/forge"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	sandboxrepo "github.com/usenorn/runner/internal/repository/sandbox"
	toolchainrepo "github.com/usenorn/runner/internal/repository/toolchain"
	toolchainsvc "github.com/usenorn/runner/internal/service/toolchain"
)

func TestEachRequiredToolIsCheckedWhereTheRunWillUseIt(t *testing.T) {
	controller := gomock.NewController(t)
	tools := toolchainrepo.NewMockToolchain(controller)
	boxes := sandboxrepo.NewMockSandbox(controller)

	tools.EXPECT().Manifests(gomock.Any(), gomock.Any()).Return([]entity.Manifest{
		{RelPath: "platform", Files: []string{"go.mod", "Makefile"}},
		{RelPath: "front", Files: []string{"package.json", "bun.lock"}},
		{RelPath: "jobs", Files: []string{"Cargo.toml"}},
	}, nil)

	located := map[string]string{
		"go":    "/snap/bin/go",
		"make":  "/usr/bin/make",
		"cargo": "/home/vlad/.cargo/bin/cargo",
	}

	tools.EXPECT().Locate(gomock.Any()).DoAndReturn(func(name string) (string, bool) {
		path, found := located[name]

		return path, found
	}).AnyTimes()

	boxes.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ entity.Sandbox, launch repository.Launch, _ time.Duration) (int, error) {
			switch launch.Command[0] {
			case "make":
				_, _ = io.WriteString(launch.Output, "GNU Make 4.4.1\nBuilt for x86_64\n")

				return 0, nil
			case "cargo":
				_, _ = io.WriteString(launch.Output, "error: rustup could not choose a version of cargo to run\n")

				return 1, nil
			}

			t.Fatalf("%s was started although it cannot run in the sandbox", launch.Command[0])

			return 0, nil
		}).
		Times(2)

	service := toolchainsvc.New(
		tools, boxes, inventoryrepo.NewMockInventory(controller), runrepo.NewMockRun(controller),
		forgerepo.NewMockForge(controller), config.Host{Timeout: time.Second}, config.Results{},
	)

	report, err := service.Check(context.Background(), entity.ToolchainProbe{
		Box: entity.Sandbox{Run: "exec-01ABC", Runtime: entity.RuntimeProcess},
	})
	if err != nil {
		t.Fatalf("check the toolchain: %v", err)
	}

	states := map[string]entity.ToolState{}
	for _, check := range report {
		states[check.Tool.Name] = check.State
	}

	want := map[string]entity.ToolState{
		"go": entity.ToolSnap, "make": entity.ToolReady, "bun": entity.ToolMissing, "cargo": entity.ToolBroken,
	}

	for name, state := range want {
		if states[name] != state {
			t.Fatalf("%s reads %q, want %q (all: %v)", name, states[name], state, states)
		}
	}

	if ready := report.Ready(); ready != "this run builds with make (GNU Make 4.4.1)" {
		t.Fatalf("the ready line reads %q", ready)
	}
}
