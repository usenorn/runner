package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usenorn/runner/internal/control"
)

type reportProgressInput struct {
	Summary string `json:"summary" jsonschema:"one line saying what you are doing now"`
	Phase   string `json:"phase,omitempty" jsonschema:"a short word for where you are, like building or testing"`
	Percent int    `json:"percent,omitempty" jsonschema:"how far along you are, 0 to 100, when you can say"`
}

type reportProgressOutput struct {
	Recorded bool `json:"recorded"`
}

func (t *toolset) reportProgress(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input reportProgressInput,
) (*mcp.CallToolResult, reportProgressOutput, error) {
	_, err := t.client.Report(ctx, t.execution, control.ProgressRequest{
		Summary: input.Summary,
		Phase:   input.Phase,
		Percent: input.Percent,
	})
	if err != nil {
		return nil, reportProgressOutput{}, toolFailure(err)
	}

	return nil, reportProgressOutput{Recorded: true}, nil
}

type publishArtifactInput struct {
	Path  string `json:"path" jsonschema:"the file to publish, relative to the workspace root"`
	Label string `json:"label,omitempty" jsonschema:"what to call it where a person sees it; defaults to the file's name"`
}

type publishArtifactOutput struct {
	ArtifactID string `json:"artifact_id"`
	Label      string `json:"label"`
	Bytes      int64  `json:"bytes"`
}

func (t *toolset) publishArtifact(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input publishArtifactInput,
) (*mcp.CallToolResult, publishArtifactOutput, error) {
	published, err := t.client.PublishArtifact(ctx, t.execution, control.ArtifactRequest{
		Path:  input.Path,
		Label: input.Label,
	})
	if err != nil {
		return nil, publishArtifactOutput{}, toolFailure(err)
	}

	return nil, publishArtifactOutput{
		ArtifactID: published.ArtifactID,
		Label:      published.Label,
		Bytes:      published.Bytes,
	}, nil
}

type completeTaskInput struct {
	Summary string `json:"summary" jsonschema:"what you changed and why, in a few sentences"`
	Notes   string `json:"notes_for_reviewer,omitempty" jsonschema:"anything whoever reviews this should know first"`
}

type completeTaskOutput struct {
	Advice string `json:"advice"`
}

func (t *toolset) completeTask(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input completeTaskInput,
) (*mcp.CallToolResult, completeTaskOutput, error) {
	completed, err := t.client.Complete(ctx, t.execution, control.CompleteRequest{
		Summary: input.Summary,
		Notes:   input.Notes,
	})
	if err != nil {
		return nil, completeTaskOutput{}, toolFailure(err)
	}

	return nil, completeTaskOutput{Advice: completed.Advice}, nil
}
