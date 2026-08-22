package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/service"
)

const listedWarnings = 5

type Snapshotting struct {
	snapshots service.Snapshots
	out       io.Writer
}

func NewSnapshotting(snapshots service.Snapshots) *Snapshotting {
	return &Snapshotting{snapshots: snapshots, out: os.Stdout}
}

type SnapshotOptions struct {
	Path         string
	IssueKey     string
	Attempt      int
	IncludeDirty bool
	JSON         bool
}

func (s *Snapshotting) Take(ctx context.Context, options SnapshotOptions) error {
	request := service.TakeRequest{
		Path:     options.Path,
		IssueKey: options.IssueKey,
		Attempt:  options.Attempt,
	}

	if options.IncludeDirty {
		request.LocalChanges = entity.LocalChangesInclude
	}

	snapshot, err := s.snapshots.Take(ctx, request)
	if err != nil {
		return err
	}

	if options.JSON {
		return s.encode(snapshot)
	}

	return s.report(snapshot)
}

func (s *Snapshotting) List(ctx context.Context, asJSON bool) error {
	snapshots, err := s.snapshots.List(ctx)
	if err != nil {
		return err
	}

	if asJSON {
		return s.encode(snapshots)
	}

	if len(snapshots) == 0 {
		return s.line("no snapshots, take one with 'norn runner snapshot take <ISSUE-KEY>'")
	}

	rows := make([][4]string, 0, len(snapshots))

	for _, snapshot := range snapshots {
		rows = append(rows, [4]string{
			snapshot.Name,
			snapshot.IssueKey,
			fmt.Sprintf("%d repositories", len(snapshot.Repositories)),
			snapshot.Run,
		})
	}

	return s.table(rows)
}

func (s *Snapshotting) Remove(ctx context.Context, name string, asJSON bool) error {
	if err := s.snapshots.Discard(ctx, name); err != nil {
		return err
	}

	if asJSON {
		return s.encode(map[string]string{"removed": name})
	}

	return s.line("removed " + name)
}

func (s *Snapshotting) report(snapshot entity.Snapshot) error {
	if err := s.line(snapshot.Name + "   " + snapshot.Workspace); err != nil {
		return err
	}

	if err := s.line(""); err != nil {
		return err
	}

	rows := make([][4]string, 0, len(snapshot.Repositories))

	for _, held := range snapshot.Repositories {
		rows = append(rows, [4]string{
			held.RelPath,
			held.Branch,
			entity.ShortSHA(held.BaseSHA),
			noteFor(held),
		})
	}

	if err := s.table(rows); err != nil {
		return err
	}

	if err := s.line(""); err != nil {
		return err
	}

	return s.summary(snapshot)
}

func (s *Snapshotting) summary(snapshot entity.Snapshot) error {
	rows := [][4]string{
		{"shared", sharedLine(snapshot), "", ""},
		{"took", snapshot.Took.Round(time.Millisecond).String(), "", ""},
	}

	if err := s.table(rows); err != nil {
		return err
	}

	return s.warnings(snapshot.Warnings)
}

func (s *Snapshotting) warnings(warnings []string) error {
	if len(warnings) == 0 {
		return nil
	}

	if err := s.line(""); err != nil {
		return err
	}

	sort.Strings(warnings)

	for index, warning := range warnings {
		if index == listedWarnings {
			return s.line(fmt.Sprintf("and %d more", len(warnings)-listedWarnings))
		}

		if err := s.line("note   " + warning); err != nil {
			return err
		}
	}

	return nil
}

func (s *Snapshotting) table(rows [][4]string) error {
	writer := tabwriter.NewWriter(s.out, 0, 0, 3, ' ', 0)

	for _, row := range rows {
		line := strings.TrimRight(strings.Join([]string{row[0], row[1], row[2], row[3]}, "\t"), "\t")

		if _, err := fmt.Fprintln(writer, line); err != nil {
			return fmt.Errorf("write the snapshot report: %w", err)
		}
	}

	return writer.Flush()
}

func (s *Snapshotting) line(text string) error {
	if _, err := fmt.Fprintln(s.out, text); err != nil {
		return fmt.Errorf("write the snapshot report: %w", err)
	}

	return nil
}

func (s *Snapshotting) encode(value any) error {
	encoder := json.NewEncoder(s.out)
	encoder.SetIndent("", "  ")

	return encoder.Encode(value)
}

func noteFor(held entity.SnapshotRepository) string {
	if held.Local == nil {
		return string(held.Mode)
	}

	return fmt.Sprintf("%s, %d local changes carried over", held.Mode, held.Local.Files)
}

func sharedLine(snapshot entity.Snapshot) string {
	cloned := 0

	for _, file := range snapshot.Shared {
		if file.Method == entity.MaterialiseReflink {
			cloned++
		}
	}

	return fmt.Sprintf(
		"%d files, %s — %d cloned, %d copied",
		len(snapshot.Shared), entity.ByteSize(snapshot.Bytes), cloned, len(snapshot.Shared)-cloned,
	)
}
