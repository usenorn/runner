package entity

import (
	"errors"
	"slices"
	"time"

	"golang.org/x/mod/semver"
)

const DevelopmentVersion = "dev"

var (
	ErrReleaseUnpublished = errors.New("norn has published no runner release yet")
	ErrReleaseUnavailable = errors.New("the runner release feed could not be reached")
)

type Build struct {
	Version     string
	Commit      string
	Modified    bool
	CommittedAt time.Time
	OS          string
	Arch        string
	Go          string
}

func (b Build) Released() bool {
	return semver.IsValid(b.Version)
}

type Release struct {
	Version     string
	URL         string
	PublishedAt time.Time
}

func (r Release) NewerThan(build Build) bool {
	if !build.Released() || !semver.IsValid(r.Version) {
		return false
	}

	return semver.Compare(r.Version, build.Version) > 0
}

type UpdateState string

const (
	UpdateOff       UpdateState = "off"
	UpdateUnchecked UpdateState = "unchecked"
	UpdateCurrent   UpdateState = "current"
	UpdateAvailable UpdateState = "available"
	UpdateUnknown   UpdateState = "unknown"
)

func UpdateStates() []UpdateState {
	return []UpdateState{
		UpdateOff,
		UpdateUnchecked,
		UpdateCurrent,
		UpdateAvailable,
		UpdateUnknown,
	}
}

func (s UpdateState) Valid() bool {
	return slices.Contains(UpdateStates(), s)
}

type Update struct {
	State     UpdateState
	Latest    string
	URL       string
	CheckedAt time.Time
	Detail    string
}
