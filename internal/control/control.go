package control

import "time"

const (
	Host = "runner"

	ReasonNotEnrolled = "not-enrolled"
	ReasonRefused     = "refused"

	StatusPath     = "/v1/status"
	ConnectPath    = "/v1/connect"
	DisconnectPath = "/v1/disconnect"
)

type Status struct {
	Version       string     `json:"version"`
	PID           int        `json:"pid"`
	StartedAt     time.Time  `json:"startedAt"`
	StateDir      string     `json:"stateDir"`
	ConfigFile    string     `json:"configFile"`
	Socket        string     `json:"socket"`
	Server        string     `json:"server"`
	Capacity      int        `json:"capacity"`
	Runtime       string     `json:"runtime"`
	Enrolled      bool       `json:"enrolled"`
	Agent         string     `json:"agent,omitempty"`
	Machine       string     `json:"machine,omitempty"`
	RunnerID      string     `json:"runnerId,omitempty"`
	Store         string     `json:"store,omitempty"`
	Session       string     `json:"session"`
	SessionDetail string     `json:"sessionDetail,omitempty"`
	Expires       *time.Time `json:"expires,omitempty"`
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
