package control

import "time"

const (
	Host       = "runner"
	StatusPath = "/v1/status"
)

type Status struct {
	Version    string    `json:"version"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"startedAt"`
	StateDir   string    `json:"stateDir"`
	ConfigFile string    `json:"configFile"`
	Socket     string    `json:"socket"`
	Server     string    `json:"server"`
	Capacity   int       `json:"capacity"`
	Runtime    string    `json:"runtime"`
	Enrolled   bool      `json:"enrolled"`
}

type Failure struct {
	Message string `json:"message"`
}
