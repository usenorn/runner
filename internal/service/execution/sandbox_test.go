package execution_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	sandboxrepo "github.com/usenorn/runner/internal/repository/sandbox"
)

type containers struct {
	mu     sync.Mutex
	opened []entity.SandboxSpec
	closed []entity.Sandbox
	swept  int
}

func (c *containers) count() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.opened), len(c.closed)
}

func dockerRuns(t *testing.T) (*sandboxrepo.MockSandbox, *containers) {
	t.Helper()

	boxes := sandboxrepo.NewMockSandbox(gomock.NewController(t))
	seen := &containers{}

	boxes.EXPECT().Check(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	boxes.EXPECT().Has(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	boxes.EXPECT().
		Tools(gomock.Any()).
		DoAndReturn(func(box entity.Sandbox) (string, error) {
			return "http://host.docker.internal:49000" + entity.RunToolsPath(box.Run), nil
		}).
		AnyTimes()
	boxes.EXPECT().
		Open(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, spec entity.SandboxSpec) error {
			seen.mu.Lock()
			defer seen.mu.Unlock()

			seen.opened = append(seen.opened, spec)

			return nil
		}).
		AnyTimes()
	boxes.EXPECT().
		Close(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, box entity.Sandbox) error {
			seen.mu.Lock()
			defer seen.mu.Unlock()

			seen.closed = append(seen.closed, box)

			return nil
		}).
		AnyTimes()
	boxes.EXPECT().
		Sweep(gomock.Any()).
		DoAndReturn(func(context.Context) error {
			seen.mu.Lock()
			defer seen.mu.Unlock()

			seen.swept++

			return nil
		}).
		AnyTimes()
	boxes.EXPECT().
		Start(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, entity.ErrDriverMissing).
		AnyTimes()
	boxes.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, entity.Sandbox, repository.Launch, time.Duration) (int, error) {
			return 0, nil
		}).
		AnyTimes()

	return boxes, seen
}

func TestARunInDockerWorksInsideItsOwnContainerAndReachesItsToolsOverTheBridge(t *testing.T) {
	boxes, seen := dockerRuns(t)
	h := newHarnessBoxed(t, boxes)

	stop := h.start(t)
	defer stop()

	offer := h.offer("exec-01ABC")
	offer.Params.Runtime = string(entity.RuntimeDocker)

	if err := h.service.Offer(context.Background(), offer); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(context.Background(), "exec-01ABC", started()); err != nil {
		t.Fatalf("start: %v", err)
	}

	h.await(t, "waited for the coding agent to start", func() bool {
		return len(h.drivers.worked()) == 1
	})

	if opened, _ := seen.count(); opened != 1 {
		t.Fatalf("the run's container was opened %d times before the agent started, want once", opened)
	}

	env := h.drivers.worked()[0]
	if env.Sandbox.Runtime != entity.RuntimeDocker || env.Sandbox.Run != "exec-01ABC" {
		t.Fatalf(
			"the coding agent was started in %+v. A run that asked for docker and then ran on "+
				"the host has none of the isolation it asked for",
			env.Sandbox,
		)
	}

	raw, err := os.ReadFile(env.MCPConfig)
	if err != nil {
		t.Fatalf("read the agent's tools: %v", err)
	}

	var config struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Command string            `json:"command"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}

	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode the agent's tools: %v", err)
	}

	norn := config.Servers[entity.ToolkitServerName]
	if norn.Type != "http" || norn.URL != "http://host.docker.internal:49000/executions/exec-01ABC/mcp" ||
		norn.Command != "" || norn.Headers["Authorization"] == "" {
		t.Fatalf(
			"norn's tools reach an agent in a container as %+v. The runner's own binary cannot "+
				"run inside the image, so they have to come over the bridge with the run's token",
			norn,
		)
	}
}

func TestTakingARunDownTakesItsContainerWithItAndARestartClearsStrays(t *testing.T) {
	first := newHarness(t, 2, 0)

	fabricate(t, first, "exec-01ABC", channelv1.StateRunning)

	task, err := first.runs.LoadTask(context.Background(), "exec-01ABC")
	if err != nil {
		t.Fatalf("read the task back: %v", err)
	}

	task.Runtime = string(entity.RuntimeDocker)

	if err := first.runs.SaveTask(context.Background(), task); err != nil {
		t.Fatalf("write the task: %v", err)
	}

	boxes, seen := dockerRuns(t)
	restarted := buildOver(t, first, boxes)

	stop := restarted.start(t)
	defer stop()

	restarted.awaitNote(t, "restarted while the run was under way")

	restarted.await(t, "waited for the interrupted run's container to go", func() bool {
		_, closed := seen.count()

		return closed == 1
	})

	seen.mu.Lock()
	defer seen.mu.Unlock()

	if seen.swept != 1 {
		t.Fatalf("a restart swept %d times for containers left behind, want once", seen.swept)
	}

	if seen.closed[0].Runtime != entity.RuntimeDocker || seen.closed[0].Run != "exec-01ABC" {
		t.Fatalf("the container taken down was %+v", seen.closed[0])
	}

	if _, err := os.Stat(filepath.Join(first.dir.Run("exec-01ABC"), entity.RunWorkspaceDir)); err == nil {
		t.Fatal("the interrupted run's workspace is still on disk")
	}
}
