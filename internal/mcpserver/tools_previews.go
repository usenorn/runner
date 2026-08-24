package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usenorn/runner/internal/control"
)

type exposePreviewInput struct {
	Service string `json:"service" jsonschema:"the service to open; it has to be one norn is running for this run"`
	Name    string `json:"name,omitempty" jsonschema:"what to call this preview where a person reads it; it is a label and never part of the address, which norn derives from the issue, the run and the port; defaults to the service's name"`
	Path    string `json:"path,omitempty" jsonschema:"the path to open at, like /admin"`
}

type previewOutput struct {
	Preview previewDTO `json:"preview"`
}

func (t *toolset) exposePreview(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input exposePreviewInput,
) (*mcp.CallToolResult, previewOutput, error) {
	exposed, err := t.client.ExposePreview(ctx, t.execution, control.PreviewRequest{
		Service: input.Service,
		Name:    input.Name,
		Path:    input.Path,
	})
	if err != nil {
		return nil, previewOutput{}, toolFailure(err)
	}

	return nil, previewOutput{Preview: previewDTOFrom(exposed)}, nil
}

type closePreviewInput struct {
	Name string `json:"name" jsonschema:"the preview to close"`
}

func (t *toolset) closePreview(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input closePreviewInput,
) (*mcp.CallToolResult, previewOutput, error) {
	closed, err := t.client.ClosePreview(ctx, t.execution, input.Name)
	if err != nil {
		return nil, previewOutput{}, toolFailure(err)
	}

	return nil, previewOutput{Preview: previewDTOFrom(closed)}, nil
}
