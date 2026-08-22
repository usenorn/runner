package control

import "time"

const (
	Host = "runner"

	ReasonNotEnrolled = "not-enrolled"
	ReasonRefused     = "refused"

	StatusPath     = "/v1/status"
	VersionPath    = "/v1/version"
	ConnectPath    = "/v1/connect"
	DisconnectPath = "/v1/disconnect"
	InspectPath    = "/v1/inspect"
	AcceptPath     = "/v1/inspect/accept"
	PausePath      = "/v1/pause"
	ResumePath     = "/v1/resume"
	ExecutionsPath = "/v1/executions"
	LogsPath       = "/v1/executions/{executionId}/logs"

	ServicesPath       = "/v1/executions/{executionId}/services"
	ServicePath        = "/v1/executions/{executionId}/services/{service}"
	ServiceRestartPath = "/v1/executions/{executionId}/services/{service}/restart"
	ServiceLogsPath    = "/v1/executions/{executionId}/services/{service}/logs"
	StepsPath          = "/v1/executions/{executionId}/steps"
)

type Update struct {
	State  string `json:"state"`
	Latest string `json:"latest,omitempty"`
	URL    string `json:"url,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type Build struct {
	Version     string     `json:"version"`
	Commit      string     `json:"commit,omitempty"`
	Modified    bool       `json:"modified,omitempty"`
	CommittedAt *time.Time `json:"committedAt,omitempty"`
	OS          string     `json:"os"`
	Arch        string     `json:"arch"`
	Go          string     `json:"go"`
	Update      Update     `json:"update"`
}

type Status struct {
	Version       string           `json:"version"`
	PID           int              `json:"pid"`
	StartedAt     time.Time        `json:"startedAt"`
	StateDir      string           `json:"stateDir"`
	ConfigFile    string           `json:"configFile"`
	Socket        string           `json:"socket"`
	Server        string           `json:"server"`
	Capacity      int              `json:"capacity"`
	Runtime       string           `json:"runtime"`
	Enrolled      bool             `json:"enrolled"`
	Agent         string           `json:"agent,omitempty"`
	Machine       string           `json:"machine,omitempty"`
	RunnerID      string           `json:"runnerId,omitempty"`
	Store         string           `json:"store,omitempty"`
	Session       string           `json:"session"`
	SessionDetail string           `json:"sessionDetail,omitempty"`
	Expires       *time.Time       `json:"expires,omitempty"`
	Codebases     []StatusCodebase `json:"codebases,omitempty"`
	Channel       Channel          `json:"channel"`
	Scheduler     Scheduler        `json:"scheduler"`
	Driver        Driver           `json:"driver"`
	Update        Update           `json:"update"`
}

type Channel struct {
	State       string     `json:"state"`
	Detail      string     `json:"detail,omitempty"`
	ConnectedAt *time.Time `json:"connectedAt,omitempty"`
	LastHeard   *time.Time `json:"lastHeard,omitempty"`
	Waiting     int        `json:"waiting"`
}

type Scheduler struct {
	Capacity   int         `json:"capacity"`
	Used       int         `json:"used"`
	Paused     bool        `json:"paused"`
	FreeDisk   *int64      `json:"freeDisk,omitempty"`
	Watermark  int64       `json:"watermark"`
	Executions []Execution `json:"executions,omitempty"`
}

type Driver struct {
	Kind      string `json:"kind"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	SignedIn  bool   `json:"signedIn"`
	Account   string `json:"account,omitempty"`
	Problem   string `json:"problem,omitempty"`
}

type Execution struct {
	ID         string     `json:"id"`
	Reference  string     `json:"reference"`
	IssueKey   string     `json:"issueKey"`
	Attempt    int        `json:"attempt"`
	Title      string     `json:"title,omitempty"`
	State      string     `json:"state"`
	Directory  string     `json:"directory"`
	Held       bool       `json:"held"`
	AcceptedAt time.Time  `json:"acceptedAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	Lease      *time.Time `json:"leaseExpiresAt,omitempty"`
}

type Health struct {
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Port    string `json:"port,omitempty"`
	Pattern string `json:"pattern,omitempty"`
}

type ServiceRequest struct {
	Name        string            `json:"name"`
	Dir         string            `json:"dir,omitempty"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment,omitempty"`
	Requires    []string          `json:"requires,omitempty"`
	Health      Health            `json:"health"`
}

type Service struct {
	Name      string     `json:"name"`
	Command   []string   `json:"command"`
	Dir       string     `json:"dir,omitempty"`
	Port      int        `json:"port,omitempty"`
	PID       int        `json:"pid,omitempty"`
	State     string     `json:"state"`
	Attempts  int        `json:"attempts,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	ChangedAt *time.Time `json:"changedAt,omitempty"`
}

type StepRequest struct {
	Name    string   `json:"name"`
	Dir     string   `json:"dir,omitempty"`
	Command []string `json:"command"`
	Timeout string   `json:"timeout,omitempty"`
}

type StepResult struct {
	Name     string `json:"name"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output,omitempty"`
	Took     string `json:"took"`
	TimedOut bool   `json:"timedOut,omitempty"`
}

type ServiceLines struct {
	Lines []string `json:"lines"`
}

type TimelineEntry struct {
	Kind     string    `json:"kind"`
	State    string    `json:"state,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	Occurred time.Time `json:"ts"`
}

type Paused struct {
	Paused bool `json:"paused"`
}

type StatusCodebase struct {
	CodebaseID   string `json:"codebaseId"`
	Name         string `json:"name"`
	RootPath     string `json:"rootPath"`
	Repositories int    `json:"repositories"`
	Drifted      bool   `json:"drifted"`
}

type Remote struct {
	Hash     string `json:"hash,omitempty"`
	Host     string `json:"host,omitempty"`
	PathTail string `json:"pathTail,omitempty"`
}

type Repository struct {
	Name          string `json:"name"`
	RelPath       string `json:"relPath"`
	Kind          string `json:"kind"`
	DefaultBranch string `json:"defaultBranch,omitempty"`
	Remote        Remote `json:"remote"`
	CommonDir     string `json:"commonDir,omitempty"`
	Parent        string `json:"parent,omitempty"`
}

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type Inventory struct {
	Name         string       `json:"name"`
	RootPath     string       `json:"rootPath"`
	Repositories []Repository `json:"repositories"`
	SharedFiles  []string     `json:"sharedFiles"`
	Runtimes     []string     `json:"runtimes"`
	Tools        []Tool       `json:"tools"`
	ScannedAt    time.Time    `json:"scannedAt"`
}

type Drift struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []string `json:"changed,omitempty"`
}

func (d Drift) Any() bool {
	return len(d.Added) > 0 || len(d.Removed) > 0 || len(d.Changed) > 0
}

type Scan struct {
	Inventory  Inventory `json:"inventory"`
	Warnings   []string  `json:"warnings,omitempty"`
	Connected  bool      `json:"connected"`
	Reconcile  bool      `json:"reconcile"`
	CodebaseID string    `json:"codebaseId,omitempty"`
	Drift      Drift     `json:"drift"`
}

type InspectRequest struct {
	Root string `json:"root"`
}

type Accepted struct {
	CodebaseID   string `json:"codebaseId"`
	Name         string `json:"name"`
	RootPath     string `json:"rootPath"`
	Repositories int    `json:"repositories"`
	Server       string `json:"server"`
}

type ConnectRequest struct {
	Token string `json:"token"`
	Name  string `json:"name,omitempty"`
	Store string `json:"store,omitempty"`
	Force bool   `json:"force,omitempty"`
}

type Connected struct {
	Agent         string     `json:"agent"`
	Machine       string     `json:"machine"`
	RunnerID      string     `json:"runnerId"`
	Server        string     `json:"server"`
	Store         string     `json:"store"`
	Session       string     `json:"session"`
	SessionDetail string     `json:"sessionDetail,omitempty"`
	Expires       *time.Time `json:"expires,omitempty"`
}

type Disconnected struct {
	Agent    string `json:"agent"`
	Machine  string `json:"machine"`
	RunnerID string `json:"runnerId"`
	Server   string `json:"server"`
}

type Failure struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
