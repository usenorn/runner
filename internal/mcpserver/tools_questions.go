package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
)

type askHumanInput struct {
	Question      string   `json:"question" jsonschema:"what you need a person to decide, in one question"`
	Options       []string `json:"options,omitempty" jsonschema:"the answers you would accept; offer these wherever you can"`
	AllowFreeText bool     `json:"allow_free_text,omitempty" jsonschema:"true when an answer that is not one of the options is fine"`
	Blocking      bool     `json:"blocking,omitempty" jsonschema:"true when you cannot go on without an answer; this is the usual case"`
	Meanwhile     string   `json:"meanwhile,omitempty" jsonschema:"what you will do if nobody answers; required when you are not blocking"`
	Kind          string   `json:"kind,omitempty" jsonschema:"decision, clarification or approval"`
	Preview       string   `json:"preview,omitempty" jsonschema:"the preview this question is about"`
	Files         []string `json:"files,omitempty" jsonschema:"the files this question is about"`
	Artifacts     []string `json:"artifacts,omitempty" jsonschema:"ids from publish_artifact this question is about"`
	WaitSeconds   int      `json:"wait_seconds,omitempty" jsonschema:"how long to hold this turn open for an answer; there is a default"`
}

type askHumanOutput struct {
	Status     string `json:"status"`
	QuestionID string `json:"question_id,omitempty"`
	Answer     string `json:"answer,omitempty"`
	AnsweredBy string `json:"answered_by,omitempty"`
	Advice     string `json:"advice,omitempty"`
}

func (t *toolset) askHuman(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input askHumanInput,
) (*mcp.CallToolResult, askHumanOutput, error) {
	answered, err := t.client.Ask(ctx, t.execution, control.QuestionRequest{
		Kind:          asking(input.Kind),
		Blocking:      input.Blocking,
		Message:       input.Question,
		Options:       input.Options,
		AllowFreeText: input.AllowFreeText || len(input.Options) == 0,
		Default:       input.Meanwhile,
		WaitSeconds:   input.WaitSeconds,
		Preview:       input.Preview,
		Files:         input.Files,
		Artifacts:     input.Artifacts,
	})
	if err != nil {
		return nil, askHumanOutput{}, toolFailure(err)
	}

	return nil, askHumanOutput{
		Status:     answered.Status,
		QuestionID: answered.QuestionID,
		Answer:     answered.Answer,
		AnsweredBy: answered.AnsweredBy,
		Advice:     answered.Advice,
	}, nil
}

func asking(kind string) string {
	if entity.QuestionKind(kind).Valid() {
		return kind
	}

	return string(entity.QuestionDecision)
}
