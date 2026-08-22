package entity

import (
	"fmt"
	"slices"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

const (
	RunLogsDir      = "logs"
	RunArtifactsDir = "artifacts"

	RunTimelineFile    = "timeline.ndjson"
	RunRepositoryFile  = "repositories.json"
	RunPermissionsFile = "permissions.json"
	RunPlanFile        = "run-plan.json"
	RunDriverFile      = "driver.json"
	RunServicesFile    = "services.json"
)

type PrepareStep string

const (
	StepCodebase PrepareStep = "work out which folder this run belongs to"
	StepSetup    PrepareStep = "work out how this run should be set up"
	StepSnapshot PrepareStep = "copy that folder into a workspace of its own"
	StepRecord   PrepareStep = "write down what it prepared"
	StepDriver   PrepareStep = "start the coding agent"
)

func Failure(step PrepareStep, err error) string {
	return fmt.Sprintf("this machine could not %s: %s", step, err)
}

type PermissionProfile string

const (
	ProfileStrict       PermissionProfile = "strict"
	ProfileStandard     PermissionProfile = "standard"
	ProfileUnrestricted PermissionProfile = "unrestricted"
)

func PermissionProfiles() []PermissionProfile {
	return []PermissionProfile{ProfileStrict, ProfileStandard, ProfileUnrestricted}
}

func (p PermissionProfile) Valid() bool {
	return slices.Contains(PermissionProfiles(), p)
}

type DriverKind string

const (
	DriverClaude DriverKind = "claude"
	DriverCodex  DriverKind = "codex"
)

func DriverKinds() []DriverKind {
	return []DriverKind{DriverClaude, DriverCodex}
}

func (k DriverKind) Valid() bool {
	return slices.Contains(DriverKinds(), k)
}

type PlanSource string

const (
	PlanNone     PlanSource = "none"
	PlanCodebase PlanSource = "codebase"
)

type RunPermissions struct {
	Profile PermissionProfile
	Chosen  string
}

type RunPlan struct {
	Source PlanSource
	Path   string
}

type RunDriver struct {
	Kind      DriverKind
	Version   string
	Installed bool
	Model     string
	Chosen    string
	Resumes   int
	Sessions  []DriverSession
}

func (d RunDriver) Latest() (DriverSession, bool) {
	if len(d.Sessions) == 0 {
		return DriverSession{}, false
	}

	return d.Sessions[len(d.Sessions)-1], true
}

type RunServices struct {
	Runtime  Runtime
	Chosen   string
	Ports    map[string]int
	Services []ServiceRecord
}

func (r RunServices) Service(name string) (ServiceRecord, bool) {
	for _, held := range r.Services {
		if held.Name == name {
			return held, true
		}
	}

	return ServiceRecord{}, false
}

type RunSetup struct {
	Permissions RunPermissions
	Plan        RunPlan
	Driver      RunDriver
	Services    RunServices
}

type TimelineEntry struct {
	Kind     channelv1.EventKind
	State    ExecutionState
	Reason   string
	Occurred time.Time
}
