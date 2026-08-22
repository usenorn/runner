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

type Executions struct {
	client *control.Client
	out    io.Writer
	now    func() time.Time
}

func NewExecutions(client *control.Client) *Executions {
	return &Executions{client: client, out: os.Stdout, now: time.Now}
}

func (e *Executions) List(ctx context.Context, asJSON bool) error {
	executions, err := e.client.Executions(ctx)
	if err != nil {
		return err
	}

	if asJSON {
		return e.encode(executions)
	}

	if len(executions) == 0 {
		return e.line("this machine is holding no runs")
	}

	rows := make([][4]string, 0, len(executions))

	for _, execution := range executions {
		rows = append(rows, [4]string{
			execution.ID, execution.Reference, execution.State, e.age(execution),
		})
	}

	return e.table(rows)
}

func (e *Executions) age(execution control.Execution) string {
	since := execution.AcceptedAt
	if execution.StartedAt != nil {
		since = *execution.StartedAt
	}

	return e.now().UTC().Sub(since).Round(time.Second).String() + " ago"
}

func (e *Executions) Logs(ctx context.Context, executionID string, asJSON bool) error {
	timeline, err := e.client.Logs(ctx, executionID)
	if err != nil {
		return err
	}

	if asJSON {
		return e.encode(timeline)
	}

	if len(timeline) == 0 {
		return e.line("nothing has happened in that run yet")
	}

	rows := make([][4]string, 0, len(timeline))

	for _, entry := range timeline {
		rows = append(rows, [4]string{
			entry.Occurred.Format(time.RFC3339), entry.Kind, entry.State, entry.Reason,
		})
	}

	return e.table(rows)
}

func (e *Executions) table(rows [][4]string) error {
	writer := tabwriter.NewWriter(e.out, 0, 0, 3, ' ', 0)

	for _, row := range rows {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\n", row[0], row[1], row[2], row[3],
		); err != nil {
			return fmt.Errorf("write the answer: %w", err)
		}
	}

	return writer.Flush()
}

func (e *Executions) line(text string) error {
	if _, err := fmt.Fprintln(e.out, text); err != nil {
		return fmt.Errorf("write the answer: %w", err)
	}

	return nil
}

func (e *Executions) encode(value any) error {
	encoder := json.NewEncoder(e.out)
	encoder.SetIndent("", "  ")

	return encoder.Encode(value)
}
