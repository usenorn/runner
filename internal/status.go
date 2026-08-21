package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/usenorn/runner/internal/control"
)

type Status struct {
	client *control.Client
	out    io.Writer
	now    func() time.Time
}

func NewStatus(client *control.Client) *Status {
	return &Status{client: client, out: os.Stdout, now: time.Now}
}

func (s *Status) Report(ctx context.Context, asJSON bool) error {
	status, err := s.client.Status(ctx)
	if err != nil {
		return err
	}

	if asJSON {
		encoder := json.NewEncoder(s.out)
		encoder.SetIndent("", "  ")

		return encoder.Encode(status)
	}

	return s.table(status)
}

func (s *Status) table(status control.Status) error {
	enrolled := "no"
	if status.Enrolled {
		enrolled = "yes"
	}

	configFile := status.ConfigFile
	if configFile == "" {
		configFile = "none, using defaults"
	}

	rows := [][2]string{
		{"runner", "running"},
		{"version", status.Version},
		{"pid", fmt.Sprint(status.PID)},
		{"uptime", s.now().UTC().Sub(status.StartedAt).Round(time.Second).String()},
		{"socket", status.Socket},
		{"state", status.StateDir},
		{"config", configFile},
		{"server", status.Server},
		{"capacity", fmt.Sprint(status.Capacity)},
		{"runtime", status.Runtime},
		{"enrolled", enrolled},
	}

	if status.Enrolled {
		rows = append(rows,
			[2]string{"agent", status.Agent},
			[2]string{"machine", status.Machine},
			[2]string{"store", status.Store},
		)
	}

	rows = append(rows, [2]string{"session", s.session(status)})

	writer := tabwriter.NewWriter(s.out, 0, 0, 3, ' ', 0)

	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s\t%s\n", row[0], row[1]); err != nil {
			return fmt.Errorf("write status: %w", err)
		}
	}

	return writer.Flush()
}

func (s *Status) session(status control.Status) string {
	line := status.Session

	if status.Expires != nil {
		line += fmt.Sprintf(
			", expires in %s", status.Expires.Sub(s.now().UTC()).Round(time.Second),
		)
	}

	if status.SessionDetail != "" {
		line += " — " + status.SessionDetail
	}

	return line
}
