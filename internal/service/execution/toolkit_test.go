package execution_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func equipped() channelv1.Start {
	lease := time.Now().UTC().Add(time.Minute)

	return channelv1.Start{
		ExecutionID:    "exec-01ABC",
		LeaseExpiresAt: &lease,
		Instructions:   "Commit small.\n\nWrite no comments.",
		Toolkit: channelv1.Toolkit{
			Skills: []channelv1.Skill{{
				Name: "release-notes", ContentHash: "4f2a", DownloadURL: "https://blobs.test/release-notes.tar.gz",
			}},
			MCPServers: []channelv1.MCPServer{
				{
					Name: "linear", Transport: "http", URL: "https://mcp.linear.test/mcp",
					Headers: map[string]string{"Authorization": "Bearer at-rae"},
				},
				{
					Name: "postgres", Transport: "stdio", Command: "pg-mcp", Args: []string{"--read-only"},
					Env: map[string]string{"DATABASE_URL": "postgres://localhost/app"},
				},
			},
		},
	}
}

func handed(t *testing.T, h *harness, start channelv1.Start) {
	t.Helper()

	ctx := context.Background()

	if err := h.service.Offer(ctx, h.offer(start.ExecutionID)); err != nil {
		t.Fatalf("offer: %v", err)
	}

	if err := h.service.Start(ctx, start.ExecutionID, start); err != nil {
		t.Fatalf("start: %v", err)
	}
}

func (h *harness) awaitFailure(t *testing.T, mentioning ...string) string {
	t.Helper()

	var reason string

	h.await(t, "waited for the run to fail before the agent started", func() bool {
		for _, reported := range h.reports(t) {
			if reported.State == string(channelv1.StateFailed) {
				reason = reported.Reason

				return true
			}
		}

		return false
	})

	for _, wanted := range mentioning {
		if !strings.Contains(reason, wanted) {
			t.Fatalf("the run failed saying %q, without %q", reason, wanted)
		}
	}

	if started := h.drivers.worked(); len(started) != 0 {
		t.Fatalf("the coding agent was started %d times for a run missing what it needs", len(started))
	}

	return reason
}

func TestTheAgentStartsWithNornsInstructionsSkillsAndEveryServerBesideNornsOwn(t *testing.T) {
	h := newHarness(t, 2, 0)

	stop := h.start(t)
	defer stop()

	handed(t, h, equipped())

	h.awaitReview(t, "exec-01ABC")

	worked := h.drivers.worked()
	if len(worked) != 1 {
		t.Fatalf("the agent was started %d times", len(worked))
	}

	env := worked[0]

	if env.Instructions != "Commit small.\n\nWrite no comments." {
		t.Errorf("the agent was given instructions %q", env.Instructions)
	}

	plugin := filepath.Join(h.dir.Run("exec-01ABC"), entity.RunMetadataDir, entity.RunToolkitDir)
	if env.Plugin != plugin {
		t.Errorf("the skills were handed over from %q, want %q", env.Plugin, plugin)
	}

	if len(h.installed) != 1 || h.installed[0] != filepath.Join(plugin, entity.ToolkitSkillsDir, "release-notes") {
		t.Errorf("skills installed at %v", h.installed)
	}

	if strings.HasPrefix(env.Plugin, env.Workspace) {
		t.Errorf("the skills were put in the workspace at %s, where a commit would pick them up", env.Plugin)
	}

	raw, err := os.ReadFile(env.MCPConfig)
	if err != nil {
		t.Fatalf("read the mcp config: %v", err)
	}

	var config struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}

	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode the mcp config: %v", err)
	}

	if norn := config.Servers[entity.ToolkitServerName]; len(norn.Args) == 0 || norn.Args[0] != "mcp-server" ||
		norn.Env[entity.ExecutionTokenVariable] == "" {
		t.Errorf("norn's own server reads %+v; without it the agent cannot report, ask or finish", norn)
	}

	if linear := config.Servers["linear"]; linear.Type != "http" || linear.Headers["Authorization"] != "Bearer at-rae" {
		t.Errorf("the http server reads %+v", linear)
	}

	if postgres := config.Servers["postgres"]; postgres.Command != "pg-mcp" ||
		postgres.Env["DATABASE_URL"] != "postgres://localhost/app" {
		t.Errorf("the stdio server reads %+v", postgres)
	}
}

func TestAServerNamedNornIsRefusedRatherThanReplacingNornsOwnTools(t *testing.T) {
	h := newHarness(t, 2, 0)

	start := equipped()
	start.Toolkit.MCPServers = append(start.Toolkit.MCPServers, channelv1.MCPServer{
		Name: "norn", Transport: "stdio", Command: "not-norn",
	})

	stop := h.start(t)
	defer stop()

	handed(t, h, start)

	h.awaitFailure(t, "the mcp server norn", entity.ErrToolkitReserved.Error())
}

func TestAServerWhoseCommandIsNotInstalledFailsTheRunAndSaysWhich(t *testing.T) {
	h := newHarness(t, 2, 0)

	if err := os.Remove(filepath.Join(h.bin, "pg-mcp")); err != nil {
		t.Fatalf("take pg-mcp off this machine: %v", err)
	}

	stop := h.start(t)
	defer stop()

	handed(t, h, equipped())

	h.awaitFailure(t, "the mcp server postgres", entity.ErrToolkitCommandMissing.Error())

	if len(h.drivers.worked()) != 0 {
		t.Fatal("the coding agent was started without a server it was promised")
	}
}

func TestASkillThatCannotBeFetchedFailsTheRunAndSaysWhich(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.skillErrs["release-notes"] = errors.New("what arrived is not what norn stored")

	stop := h.start(t)
	defer stop()

	handed(t, h, equipped())

	h.awaitFailure(t, "the skill release-notes", "what arrived is not what norn stored")
}

func TestARunThatCannotReachNornsToolsDoesNotStart(t *testing.T) {
	h := newHarness(t, 2, 0)
	h.nornErr = errors.New("norn's tools refused this machine")

	stop := h.start(t)
	defer stop()

	begun(t, h, "exec-01ABC")

	h.awaitFailure(t, "the mcp server norn", "refused this machine")
}
