package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usenorn/runner/internal/control"
)

type refreshRemoteInput struct{}

func (t *toolset) refreshRemote(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ refreshRemoteInput,
) (*mcp.CallToolResult, control.RemoteRefresh, error) {
	remote, err := t.client.RefreshRemote(ctx, t.execution)
	if err != nil {
		return nil, control.RemoteRefresh{}, toolFailure(err)
	}

	return nil, remote, nil
}
