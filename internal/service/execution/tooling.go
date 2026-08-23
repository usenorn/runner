package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

const mcpServerName = "norn"

type mcpServer struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Environment map[string]string `json:"env,omitempty"`
}

type mcpConfig struct {
	Servers map[string]mcpServer `json:"mcpServers"`
}

func (s *executionsService) tooling(
	ctx context.Context,
	execution entity.Execution,
	snapshot entity.Snapshot,
	setup entity.RunSetup,
) (entity.ExecEnv, error) {
	token, err := s.tokens.Mint(ctx, execution.ID)
	if err != nil {
		return entity.ExecEnv{}, err
	}

	binary, err := os.Executable()
	if err != nil {
		return entity.ExecEnv{}, fmt.Errorf("find the norn this daemon is running from: %w", err)
	}

	raw, err := json.MarshalIndent(mcpConfig{Servers: map[string]mcpServer{
		mcpServerName: {
			Command: binary,
			Args:    []string{"mcp-server", "--exec", execution.ID},
			Environment: map[string]string{
				entity.ExecutionVariable:      execution.ID,
				entity.ExecutionTokenVariable: token,
			},
		},
	}}, "", "  ")
	if err != nil {
		return entity.ExecEnv{}, fmt.Errorf("write the tools for %s: %w", execution.ID, err)
	}

	path := filepath.Join(execution.Metadata(), entity.RunMCPFile)

	if err := statedir.WriteSecret(path, append(raw, '\n')); err != nil {
		return entity.ExecEnv{}, fmt.Errorf("write the tools for %s: %w", execution.ID, err)
	}

	return s.env(execution, snapshot, setup, token, path), nil
}
