package mcpserver

import (
	"time"

	"github.com/usenorn/runner/internal/control"
)

type serviceDTO struct {
	Name      string     `json:"name"`
	State     string     `json:"state"`
	Port      int        `json:"port,omitempty"`
	Command   []string   `json:"command,omitempty"`
	Dir       string     `json:"dir,omitempty"`
	Attempts  int        `json:"attempts,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
}

func serviceDTOFrom(service control.Service) serviceDTO {
	return serviceDTO{
		Name:      service.Name,
		State:     service.State,
		Port:      service.Port,
		Command:   service.Command,
		Dir:       service.Dir,
		Attempts:  service.Attempts,
		Reason:    service.Reason,
		StartedAt: service.StartedAt,
	}
}

type previewDTO struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	Path    string `json:"path,omitempty"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
	Reach   string `json:"reach"`
}

const localReach = "this machine only; the shared address arrives with norn's preview tunnel"

func previewDTOFrom(preview control.Preview) previewDTO {
	return previewDTO{
		Name:    preview.Name,
		Service: preview.Service,
		Path:    preview.Path,
		Port:    preview.Port,
		URL:     preview.URL,
		Reach:   localReach,
	}
}
