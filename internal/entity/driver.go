package entity

import (
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	DriverLineMax    = 1 << 20
	DriverTextMax    = 16 << 10
	DriverPayloadMax = 16 << 10
	DriverTruncated  = "… [cut here: the coding agent said more than norn keeps in one entry]"

	ExecutionVariable      = "NORN_EXEC_ID"
	ExecutionTokenVariable = "NORN_EXEC_TOKEN"

	AgentTokenAccount = "the token kept with 'norn runner agent-token'"
)

var (
	ErrDriverUnsupported = errors.New(
		"this release drives claude code only; nothing else can run the work yet",
	)
	ErrDriverMissing = errors.New(
		"the coding agent is not installed on this machine",
	)
	ErrDriverCrashed = errors.New(
		"the coding agent stopped before it said it was finished",
	)
	ErrDriverUnanswerable = errors.New(
		"the coding agent stopped for something it wanted to be asked directly, which nothing " +
			"outside its own session can answer; a question for a person goes through 'norn ask'",
	)
	ErrAgentTokenMissing = errors.New(
		"this machine has no token for the coding agent; run 'claude setup-token' and hand what " +
			"it prints to 'norn runner agent-token'",
	)
	ErrAgentTokenMalformed = errors.New(
		"that is not a coding agent token; paste exactly what 'claude setup-token' printed",
	)
	ErrDriverSessionUnknown = errors.New(
		"this run has no coding agent session to carry on from",
	)

	ErrUploadPositionTaken = errors.New(
		"norn already holds a different batch at that position in the stream",
	)
	ErrUploadTooLarge = errors.New(
		"norn takes less in one batch than this machine tried to send",
	)
	ErrUploadRefused = errors.New(
		"norn would not take that batch",
	)
	ErrUploadUnknownRun = errors.New(
		"norn does not have this machine down as holding that run",
	)
)

type DriverEventKind string

const (
	DriverEventMessage    DriverEventKind = "message"
	DriverEventToolCall   DriverEventKind = "tool_call"
	DriverEventToolResult DriverEventKind = "tool_result"
	DriverEventUsage      DriverEventKind = "usage"
	DriverEventError      DriverEventKind = "error"
	DriverEventNeedsInput DriverEventKind = "needs_input"
)

func DriverEventKinds() []DriverEventKind {
	return []DriverEventKind{
		DriverEventMessage, DriverEventToolCall, DriverEventToolResult, DriverEventUsage,
		DriverEventError, DriverEventNeedsInput,
	}
}

func (k DriverEventKind) Valid() bool {
	return slices.Contains(DriverEventKinds(), k)
}

type DriverOutcome string

const (
	OutcomeDone       DriverOutcome = "done"
	OutcomeFailed     DriverOutcome = "error"
	OutcomeNeedsInput DriverOutcome = "needs_input"
	OutcomeCrashed    DriverOutcome = "crashed"
)

func DriverOutcomes() []DriverOutcome {
	return []DriverOutcome{OutcomeDone, OutcomeFailed, OutcomeNeedsInput, OutcomeCrashed}
}

func (o DriverOutcome) Valid() bool {
	return slices.Contains(DriverOutcomes(), o)
}

func (o DriverOutcome) Finished() bool {
	return o == OutcomeDone
}

type DriverEvent struct {
	Kind    DriverEventKind
	At      time.Time
	Text    string
	Tool    string
	Payload map[string]any
}

type DriverUsage struct {
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	Turns        int
	Took         time.Duration
}

type DriverResult struct {
	Outcome  DriverOutcome
	Summary  string
	Usage    DriverUsage
	Denials  int
	ExitCode int
}

type DriverSession struct {
	ID        string
	StartedAt time.Time
	EndedAt   time.Time
	Outcome   DriverOutcome
	Reason    string
}

type DriverHealth struct {
	Kind      DriverKind
	Installed bool
	Version   string
	SignedIn  bool
	Account   string
	Problem   string
}

func (h DriverHealth) Ready() bool {
	return h.Installed && h.SignedIn
}

func (h DriverHealth) FaultIn(runtime Runtime) error {
	if runtime == RuntimeDocker && !h.SignedIn {
		return ErrAgentTokenMissing
	}

	if runtime == RuntimeDocker {
		return nil
	}

	return h.Fault()
}

func (h DriverHealth) Fault() error {
	switch {
	case !h.Installed:
		return ErrDriverMissing
	case !h.SignedIn:
		return ErrAgentTokenMissing
	default:
		return nil
	}
}

type ExecEnv struct {
	ExecutionID  string
	Sandbox      Sandbox
	AgentToken   string
	Workspace    string
	Environment  []string
	MCPConfig    string
	Instructions string
	Plugin       string
	Profile      PermissionProfile
}

type Task struct {
	Prompt string
	Model  string
}

type UploadStream string

const (
	StreamLogs       UploadStream = "logs"
	StreamTranscript UploadStream = "transcript"
)

func UploadStreams() []UploadStream {
	return []UploadStream{StreamLogs, StreamTranscript}
}

func (s UploadStream) Valid() bool {
	return slices.Contains(UploadStreams(), s)
}

type TelemetryMode string

const (
	TelemetryFull    TelemetryMode = "full"
	TelemetryMinimal TelemetryMode = "minimal"
)

func (m TelemetryMode) Keeps(stream UploadStream) bool {
	return m != TelemetryMinimal || stream != StreamTranscript
}

type LogLine struct {
	At     time.Time
	Stream string
	Source string
	Text   string
}

type LogBatch struct {
	Sequence int64
	Entries  []LogLine
}

type TranscriptBatch struct {
	Sequence int64
	Entries  []DriverEvent
}

type StreamCursor struct {
	Stream       UploadStream
	LastSequence int64
	Chunks       int
	Entries      int64
	Bytes        int64
}

type UploadReceipt struct {
	Stream    UploadStream
	Sequence  int64
	Digest    string
	Duplicate bool
}

const DriverResumeInjection = "Your session stopped before you said you were finished. Carry on " +
	"from where you left off: check what you had already changed in this workspace, then finish " +
	"the work and commit it."

func ValidateAgentToken(token string) error {
	if token == "" || strings.ContainsFunc(token, unicode.IsSpace) {
		return ErrAgentTokenMalformed
	}

	return nil
}
