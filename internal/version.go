package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/service"
)

type Version struct {
	client  *control.Client
	updates service.Updates
	build   entity.Build
	out     io.Writer
}

func NewVersion(client *control.Client, updates service.Updates, build entity.Build) *Version {
	return &Version{client: client, updates: updates, build: build, out: os.Stdout}
}

type versionReport struct {
	control.Build

	Daemon string `json:"daemon,omitempty"`
}

func (v *Version) Report(ctx context.Context, asJSON, check bool) error {
	report := versionReport{Build: control.Build{
		Version:  v.build.Version,
		Commit:   v.build.Commit,
		Modified: v.build.Modified,
		OS:       v.build.OS,
		Arch:     v.build.Arch,
		Go:       v.build.Go,
	}}

	if !v.build.CommittedAt.IsZero() {
		committed := v.build.CommittedAt
		report.CommittedAt = &committed
	}

	running, err := v.client.Version(ctx)
	switch {
	case err == nil:
		report.Daemon = running.Version
		report.Update = running.Update
	case !errors.Is(err, entity.ErrDaemonUnavailable):
		return err
	case check:
		report.Update = updateOf(v.updates.Check(ctx))
	default:
		report.Update = updateOf(v.updates.Report())
	}

	if asJSON {
		encoder := json.NewEncoder(v.out)
		encoder.SetIndent("", "  ")

		return encoder.Encode(report)
	}

	return v.table(report)
}

func (v *Version) table(report versionReport) error {
	rows := [][2]string{{"runner", report.Version}}

	if report.Commit != "" {
		commit := report.Commit
		if report.Modified {
			commit += " (built from a modified tree)"
		}

		rows = append(rows, [2]string{"commit", commit})
	}

	if report.CommittedAt != nil {
		rows = append(rows, [2]string{"committed", report.CommittedAt.UTC().Format(time.RFC3339)})
	}

	rows = append(rows,
		[2]string{"platform", report.OS + "/" + report.Arch},
		[2]string{"go", report.Go},
	)

	if line := daemonLine(report); line != "" {
		rows = append(rows, [2]string{"daemon", line})
	}

	rows = append(rows, [2]string{"update", updateLine(report.Update)})

	writer := tabwriter.NewWriter(v.out, 0, 0, 3, ' ', 0)

	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s\t%s\n", row[0], row[1]); err != nil {
			return fmt.Errorf("write the version report: %w", err)
		}
	}

	return writer.Flush()
}

func daemonLine(report versionReport) string {
	switch report.Daemon {
	case "":
		return "not running"
	case report.Version:
		return report.Daemon
	default:
		return fmt.Sprintf(
			"%s, still running the build it started from; restart it to pick up %s",
			report.Daemon, report.Version,
		)
	}
}

func updateLine(update control.Update) string {
	switch entity.UpdateState(update.State) {
	case entity.UpdateAvailable:
		return fmt.Sprintf("%s is available — %s", update.Latest, update.URL)
	case entity.UpdateCurrent:
		return "this is the newest release"
	default:
		return withDetail(update)
	}
}

func withDetail(update control.Update) string {
	if update.Detail == "" {
		return update.State
	}

	return update.State + " — " + update.Detail
}

func updateOf(update entity.Update) control.Update {
	return control.Update{
		State:  string(update.State),
		Latest: update.Latest,
		URL:    update.URL,
		Detail: update.Detail,
	}
}
