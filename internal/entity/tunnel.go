package entity

import (
	"errors"
	"slices"
	"time"
)

var (
	ErrTunnelUnknownPair = errors.New("this machine is not running that preview")
	ErrTunnelCrowded     = errors.New("this machine is already carrying as much as it may")
)

type TunnelState string

const (
	TunnelOff        TunnelState = "off"
	TunnelConnecting TunnelState = "connecting"
	TunnelLive       TunnelState = "live"
	TunnelOffline    TunnelState = "offline"
	TunnelRefused    TunnelState = "refused"
	TunnelUnserved   TunnelState = "unserved"
)

func TunnelStates() []TunnelState {
	return []TunnelState{
		TunnelOff, TunnelConnecting, TunnelLive, TunnelOffline, TunnelRefused, TunnelUnserved,
	}
}

func (s TunnelState) Valid() bool {
	return slices.Contains(TunnelStates(), s)
}

func (s TunnelState) Settled() bool {
	return s == TunnelRefused || s == TunnelUnserved
}

func TunnelStateFor(err error) TunnelState {
	switch {
	case err == nil:
		return TunnelLive
	case errors.Is(err, ErrPreviewsUnserved):
		return TunnelUnserved
	case errors.Is(err, ErrRunnerRevoked),
		errors.Is(err, ErrAgentDisabled),
		errors.Is(err, ErrCredentialInvalid):
		return TunnelRefused
	case errors.Is(err, ErrNotEnrolled):
		return TunnelOff
	default:
		return TunnelOffline
	}
}

type TunnelReport struct {
	State       TunnelState
	Gateway     string
	ConnectedAt time.Time
	Streams     int
	Detail      string
}

type GatewayReach string

const (
	GatewayReachable    GatewayReach = "reachable"
	GatewayUnreachable  GatewayReach = "unreachable"
	GatewayUnconfigured GatewayReach = "unconfigured"
)

func GatewayReaches() []GatewayReach {
	return []GatewayReach{GatewayReachable, GatewayUnreachable, GatewayUnconfigured}
}

func (r GatewayReach) Valid() bool {
	return slices.Contains(GatewayReaches(), r)
}
