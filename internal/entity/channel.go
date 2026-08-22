package entity

import (
	"errors"
	"fmt"
	"slices"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

const (
	ChannelPath     = "/v1/runners/channel"
	ChannelProtocol = channelv1.Version
	ChannelPulse    = channelv1.Heartbeat
)

var (
	ErrChannelOff         = errors.New("this norn is not accepting runner channels")
	ErrChannelDisplaced   = errors.New("another connection took this machine's channel")
	ErrChannelClosed      = errors.New("norn closed the channel")
	ErrChannelUnsupported = errors.New("norn refused a message this machine sent")
	ErrRunnerOutdated     = errors.New("this build of the runner is older than norn will talk to")
)

type OutdatedError struct {
	Detail string
}

func (e OutdatedError) Error() string {
	if e.Detail == "" {
		return ErrRunnerOutdated.Error()
	}

	return e.Detail
}

func (e OutdatedError) Unwrap() error {
	return ErrRunnerOutdated
}

type ChannelState string

const (
	ChannelOff        ChannelState = "off"
	ChannelConnecting ChannelState = "connecting"
	ChannelLive       ChannelState = "live"
	ChannelOffline    ChannelState = "offline"
	ChannelRefused    ChannelState = "refused"
)

func ChannelStates() []ChannelState {
	return []ChannelState{
		ChannelOff, ChannelConnecting, ChannelLive, ChannelOffline, ChannelRefused,
	}
}

func (s ChannelState) Valid() bool {
	return slices.Contains(ChannelStates(), s)
}

func (s ChannelState) Settled() bool {
	return s == ChannelRefused
}

type ChannelReport struct {
	State       ChannelState
	Detail      string
	ConnectedAt time.Time
	LastHeard   time.Time
	Waiting     int
}

func ChannelStateFor(err error) ChannelState {
	switch {
	case errors.Is(err, ErrRunnerOutdated),
		errors.Is(err, ErrRunnerRevoked),
		errors.Is(err, ErrAgentDisabled),
		errors.Is(err, ErrChannelOff):
		return ChannelRefused
	case errors.Is(err, ErrNotEnrolled):
		return ChannelOff
	default:
		return ChannelOffline
	}
}

type DeclineReason string

const (
	DeclineAtCapacity   DeclineReason = channelv1.DeclineAtCapacity
	DeclineDiskPressure DeclineReason = channelv1.DeclineDiskPressure
	DeclinePaused       DeclineReason = channelv1.DeclinePaused
)

func DeclineReasons() []DeclineReason {
	return []DeclineReason{DeclineAtCapacity, DeclineDiskPressure, DeclinePaused}
}

func (r DeclineReason) Valid() bool {
	return slices.Contains(DeclineReasons(), r)
}

func (r DeclineReason) Because(report SchedulerReport) string {
	switch r {
	case DeclineAtCapacity:
		return fmt.Sprintf(
			"this machine is already holding %d of %d executions", report.Used, report.Capacity,
		)
	case DeclineDiskPressure:
		return fmt.Sprintf(
			"this machine has %s free and keeps %s back",
			ByteSize(report.Room.Free), ByteSize(report.Room.Watermark),
		)
	case DeclinePaused:
		return "this machine has been paused and is not taking work"
	default:
		return string(r)
	}
}
