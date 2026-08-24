package entity

import (
	"errors"
	"path/filepath"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

const ExecutionTaskFile = "task.json"

var (
	ErrExecutionUnknown = errors.New("this machine is not holding that execution")
	ErrExecutionRefused = errors.New("an execution cannot move between those states")

	ErrExecutionNoCodebase = errors.New(
		"this machine has no connected folder to run the work in; connect one with " +
			"'norn runner inspect'",
	)
	ErrExecutionManyCodebases = errors.New(
		"this machine has more than one connected folder and nothing says which one the work " +
			"belongs to; this release runs one folder per machine",
	)
)

type ExecutionState = channelv1.State

type Execution struct {
	ID           string
	Reference    string
	IssueKey     string
	Branch       string
	Attempt      int
	WorkspaceID  string
	Title        string
	Description  string
	Brief        string
	Tool         string
	Model        string
	Runtime      string
	BaseRef      string
	IncludeDirty bool
	Profile      string
	Directory    string
	State        ExecutionState
	Lease        time.Time
	AcceptedAt   time.Time
	StartedAt    time.Time
	SettledAt    time.Time
	KeepUntil    time.Time
}

func ExecutionOf(offer channelv1.Offer, root string, acceptedAt time.Time) Execution {
	return Execution{
		ID:           offer.ExecutionID,
		Reference:    offer.Reference,
		IssueKey:     offer.Issue.Reference,
		Branch:       offer.Branch,
		Attempt:      max(offer.Attempt, 1),
		WorkspaceID:  offer.WorkspaceID,
		Title:        offer.Issue.Title,
		Description:  offer.Issue.Description,
		Brief:        offer.Issue.Brief,
		Tool:         offer.Params.Tool,
		Model:        offer.Params.Model,
		Runtime:      offer.Params.Runtime,
		BaseRef:      offer.Params.BaseRef,
		IncludeDirty: offer.Params.IncludeDirty,
		Profile:      offer.Params.Profile,
		Directory:    filepath.Join(root, offer.ExecutionID),
		State:        channelv1.StateLeased,
		AcceptedAt:   acceptedAt,
	}
}

func (e Execution) Metadata() string {
	return filepath.Join(e.Directory, RunMetadataDir)
}

func (e Execution) HoldsSlot() bool {
	return e.State.HoldsSlot()
}

func (e Execution) Finished() bool {
	return e.State.Terminal()
}

func (e Execution) Reported() bool {
	return e.Finished() && e.State.RunnerDriven()
}

func (e Execution) CanReport(state ExecutionState) bool {
	return state.RunnerDriven() && e.State.CanTransitionTo(state)
}

type Room struct {
	Free      int64
	Watermark int64
	Known     bool
}

func (r Room) Pressed() bool {
	return r.Known && r.Watermark > 0 && r.Free < r.Watermark
}

type SchedulerReport struct {
	Capacity   int
	Used       int
	Paused     bool
	Room       Room
	Runs       RunsReport
	Executions []Execution
}

func (r SchedulerReport) Full() bool {
	return r.Used >= r.Capacity
}

func (r SchedulerReport) Decline() (DeclineReason, bool) {
	switch {
	case r.Paused:
		return DeclinePaused, true
	case r.Full():
		return DeclineAtCapacity, true
	case r.Room.Pressed():
		return DeclineDiskPressure, true
	default:
		return "", false
	}
}
