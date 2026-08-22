package driver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

type envelope struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	Timestamp time.Time       `json:"timestamp"`
	Message   json.RawMessage `json:"message"`

	Model      string  `json:"model"`
	IsError    bool    `json:"is_error"`
	Result     string  `json:"result"`
	NumTurns   int     `json:"num_turns"`
	DurationMS int64   `json:"duration_ms"`
	CostUSD    float64 `json:"total_cost_usd"`
	Usage      struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
	Denials []json.RawMessage `json:"permission_denials"`
}

type message struct {
	Role    string  `json:"role"`
	Content []block `json:"content"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

const (
	kindSystem = "system"
	kindResult = "result"

	subtypeInit    = "init"
	subtypeSuccess = "success"

	roleAssistant = "assistant"
	roleUser      = "user"

	blockText       = "text"
	blockToolUse    = "tool_use"
	blockToolResult = "tool_result"
)

type reading struct {
	tools map[string]string
}

func newReading() *reading {
	return &reading{tools: map[string]string{}}
}

func (r *reading) read(line []byte, at time.Time) ([]entity.DriverEvent, *entity.DriverResult, error) {
	var held envelope

	if err := json.Unmarshal(line, &held); err != nil {
		return nil, nil, fmt.Errorf("read what the coding agent said: %w", err)
	}

	stamp := held.Timestamp
	if stamp.IsZero() {
		stamp = at
	}

	stamp = stamp.UTC()

	switch held.Type {
	case kindSystem:
		return nil, nil, nil
	case roleAssistant, roleUser:
		return r.spoke(held, stamp), nil, nil
	case kindResult:
		return []entity.DriverEvent{spend(held, stamp)}, finished(held), nil
	default:
		return nil, nil, nil
	}
}

func (r *reading) session(line []byte) string {
	var held envelope

	if err := json.Unmarshal(line, &held); err != nil {
		return ""
	}

	if held.Type == kindSystem && held.Subtype == subtypeInit {
		return held.SessionID
	}

	return ""
}

func (r *reading) spoke(held envelope, at time.Time) []entity.DriverEvent {
	var body message

	if err := json.Unmarshal(held.Message, &body); err != nil {
		return nil
	}

	events := make([]entity.DriverEvent, 0, len(body.Content))

	for _, held := range body.Content {
		switch held.Type {
		case blockText:
			if strings.TrimSpace(held.Text) == "" {
				continue
			}

			events = append(events, entity.DriverEvent{
				Kind: entity.DriverEventMessage,
				At:   at,
				Text: cut(held.Text, entity.DriverTextMax),
			})
		case blockToolUse:
			r.tools[held.ID] = held.Name

			events = append(events, entity.DriverEvent{
				Kind:    entity.DriverEventToolCall,
				At:      at,
				Tool:    held.Name,
				Payload: payload(held.Input),
			})
		case blockToolResult:
			events = append(events, entity.DriverEvent{
				Kind:    entity.DriverEventToolResult,
				At:      at,
				Tool:    r.tools[held.ToolUseID],
				Text:    cut(flatten(held.Content), entity.DriverTextMax),
				Payload: map[string]any{"failed": held.IsError},
			})
		}
	}

	return events
}

func spend(held envelope, at time.Time) entity.DriverEvent {
	return entity.DriverEvent{
		Kind: entity.DriverEventUsage,
		At:   at,
		Payload: map[string]any{
			"input_tokens":  held.Usage.InputTokens,
			"output_tokens": held.Usage.OutputTokens,
			"cost_usd":      held.CostUSD,
			"turns":         held.NumTurns,
			"duration_ms":   held.DurationMS,
		},
	}
}

func finished(held envelope) *entity.DriverResult {
	outcome := entity.OutcomeDone
	if held.IsError || held.Subtype != subtypeSuccess {
		outcome = entity.OutcomeFailed
	}

	return &entity.DriverResult{
		Outcome: outcome,
		Summary: cut(held.Result, entity.DriverTextMax),
		Usage: entity.DriverUsage{
			InputTokens:  held.Usage.InputTokens,
			OutputTokens: held.Usage.OutputTokens,
			CostUSD:      held.CostUSD,
			Turns:        held.NumTurns,
			Took:         time.Duration(held.DurationMS) * time.Millisecond,
		},
		Denials: len(held.Denials),
	}
}

func payload(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || len(raw) > entity.DriverPayloadMax {
		return map[string]any{"omitted": "what this tool was given is longer than norn keeps"}
	}

	held := map[string]any{}

	if err := json.Unmarshal(raw, &held); err != nil {
		return nil
	}

	return held
}

func flatten(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string

	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}

	var blocks []block

	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}

	parts := make([]string, 0, len(blocks))

	for _, held := range blocks {
		if held.Text != "" {
			parts = append(parts, held.Text)
		}
	}

	return strings.Join(parts, "\n")
}

func cut(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	return value[:limit] + entity.DriverTruncated
}
