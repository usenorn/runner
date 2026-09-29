package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

type mcpServer struct {
	Type        string            `json:"type,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Environment map[string]string `json:"env,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

type mcpConfig struct {
	Servers map[string]mcpServer `json:"mcpServers"`
}

func (s *executionsService) equip(ctx context.Context, execution entity.Execution) error {
	toolkit, err := s.runs.LoadToolkit(ctx, execution.ID)
	if err != nil {
		return err
	}

	gaps := toolkit.Gaps()
	plugin := filepath.Join(execution.Metadata(), entity.RunToolkitDir)

	for _, skill := range toolkit.Skills {
		if !skill.Valid() {
			continue
		}

		if err := s.toolkits.InstallSkill(ctx, skill, plugin); err != nil {
			gaps = append(gaps, entity.ToolkitGap{Kind: entity.ToolkitSkillKind, Name: skill.Name, Reason: err})
		}
	}

	if err := s.reachNorn(ctx); err != nil {
		gaps = append(gaps, entity.ToolkitGap{
			Kind: entity.ToolkitServerKind, Name: entity.ToolkitServerName, Reason: err,
		})
	}

	if err := entity.Shortfall(gaps); err != nil {
		return err
	}

	return s.note(ctx, execution.ID, channelv1.EventPhase, equipped(toolkit))
}

func (s *executionsService) commands(ctx context.Context, execution entity.Execution) error {
	toolkit, err := s.runs.LoadToolkit(ctx, execution.ID)
	if err != nil {
		return err
	}

	gaps := []entity.ToolkitGap{}

	for _, server := range toolkit.Servers {
		if server.Transport == entity.ToolkitStdio && !s.sandboxes.Has(ctx, execution.Sandbox(), server.Command) {
			gaps = append(gaps, entity.ToolkitGap{
				Kind: entity.ToolkitServerKind, Name: server.Name, Reason: entity.ErrToolkitCommandMissing,
			})
		}
	}

	return entity.Shortfall(gaps)
}

func (s *executionsService) reachNorn(ctx context.Context) error {
	token, err := s.access.Access(ctx)
	if err != nil {
		return err
	}

	return s.toolkits.ReachNorn(ctx, token)
}

func equipped(toolkit entity.Toolkit) string {
	told := fmt.Sprintf(
		"the coding agent has norn's tools, %d skills and %d more mcp servers",
		len(toolkit.Skills), len(toolkit.Servers),
	)

	if toolkit.Instructions == "" {
		return told + ", and no instructions from norn"
	}

	return told + ", and norn's instructions for it"
}

func (s *executionsService) tooling(
	ctx context.Context,
	execution entity.Execution,
	snapshot entity.Snapshot,
	setup entity.RunSetup,
) (entity.ExecEnv, error) {
	toolkit, err := s.runs.LoadToolkit(ctx, execution.ID)
	if err != nil {
		return entity.ExecEnv{}, err
	}

	if err := entity.Shortfall(toolkit.Gaps()); err != nil {
		return entity.ExecEnv{}, err
	}

	token, err := s.tokens.Mint(ctx, execution.ID)
	if err != nil {
		return entity.ExecEnv{}, err
	}

	binary, err := os.Executable()
	if err != nil {
		return entity.ExecEnv{}, fmt.Errorf("find the norn this daemon is running from: %w", err)
	}

	servers := make(map[string]mcpServer, len(toolkit.Servers)+1)

	for _, server := range toolkit.Servers {
		servers[server.Name] = configured(server)
	}

	servers[entity.ToolkitServerName] = mcpServer{
		Command: binary,
		Args:    []string{"mcp-server", "--exec", execution.ID},
		Environment: map[string]string{
			entity.ExecutionVariable:      execution.ID,
			entity.ExecutionTokenVariable: token,
		},
	}

	reach, err := s.sandboxes.Tools(execution.Sandbox())
	if err != nil {
		return entity.ExecEnv{}, err
	}

	if reach != "" {
		servers[entity.ToolkitServerName] = mcpServer{
			Type:    entity.ToolkitHTTP,
			URL:     reach,
			Headers: map[string]string{"Authorization": "Bearer " + token},
		}
	}

	raw, err := json.MarshalIndent(mcpConfig{Servers: servers}, "", "  ")
	if err != nil {
		return entity.ExecEnv{}, fmt.Errorf("write the tools for %s: %w", execution.ID, err)
	}

	path := filepath.Join(execution.Metadata(), entity.RunMCPFile)

	if err := statedir.WriteSecret(path, append(raw, '\n')); err != nil {
		return entity.ExecEnv{}, fmt.Errorf("write the tools for %s: %w", execution.ID, err)
	}

	agentToken, err := s.agentToken(ctx)
	if err != nil {
		return entity.ExecEnv{}, err
	}

	env := s.env(execution, snapshot, setup, token, path)
	env.AgentToken = agentToken
	env.Instructions = toolkit.Instructions

	if len(toolkit.Skills) > 0 {
		env.Plugin = filepath.Join(execution.Metadata(), entity.RunToolkitDir)
	}

	return env, nil
}

func configured(server entity.ToolkitServer) mcpServer {
	if server.Transport == entity.ToolkitStdio {
		return mcpServer{Command: server.Command, Args: server.Args, Environment: server.Env}
	}

	return mcpServer{Type: server.Transport, URL: server.URL, Headers: server.Headers}
}
