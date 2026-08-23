package run_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	runrepo "github.com/usenorn/runner/internal/repository/run"
)

func TestWhatARunTakesUpCountsEverythingUnderIt(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	path, err := runs.Open(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("open a run: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(path, entity.RunWorkspaceDir, "big"), make([]byte, 4096), 0o600,
	); err != nil {
		t.Fatalf("write into the workspace: %v", err)
	}

	usage, err := runs.Usage(ctx)
	if err != nil {
		t.Fatalf("measure the runs: %v", err)
	}

	if len(usage) != 1 {
		t.Fatalf("usage = %+v, want one run", usage)
	}

	if usage[0].Bytes < 4096 {
		t.Fatalf(
			"usage[0].Bytes = %d, want at least 4096. A figure that misses what the coding agent "+
				"wrote would let the disk fill while the machine says it is under budget",
			usage[0].Bytes,
		)
	}

	if !usage[0].Workspace {
		t.Fatalf("usage[0] says it has nothing left to give back, but its workspace is there")
	}
}

func TestARunDirectoryWithNoTaskIsMeasuredAndNeverCalledFinished(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Open(ctx, "snap-NORN-55-1"); err != nil {
		t.Fatalf("open a snapshot taken by hand: %v", err)
	}

	usage, err := runs.Usage(ctx)
	if err != nil {
		t.Fatalf("measure the runs: %v", err)
	}

	if len(usage) != 1 || usage[0].Finished || !usage[0].Settled.IsZero() {
		t.Fatalf(
			"usage = %+v, want one unfinished run with no settle time. Calling a snapshot "+
				"somebody took by hand finished would let the sweep delete it",
			usage,
		)
	}
}

func TestMeasuringRunsOnAMachineThatHasHeldNoneAnswersWithNone(t *testing.T) {
	dir, ctx := store(t)

	if err := os.RemoveAll(dir.Runs()); err != nil {
		t.Fatalf("take the runs directory away: %v", err)
	}

	usage, err := runrepo.New(dir).Usage(ctx)
	if err != nil {
		t.Fatalf(
			"a machine that has never held a run could not be asked what its runs take up: %v",
			err,
		)
	}

	if len(usage) != 0 {
		t.Fatalf("usage = %+v, want none", usage)
	}
}

func TestRetiringARunLeavesWhatExplainsItAndTakesTheRunTokenWithIt(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	path, err := runs.Open(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("open a run: %v", err)
	}

	written := map[string]string{
		filepath.Join(path, entity.RunWorkspaceDir, "code.go"):               "package main",
		filepath.Join(path, entity.RunArtifactsDir, "shot.png"):              "image",
		filepath.Join(path, entity.RunMetadataDir, entity.RunMCPFile):        "{}",
		filepath.Join(path, entity.RunMetadataDir, entity.ExecutionTaskFile): "{}",
		filepath.Join(path, entity.RunLogsDir, entity.RunTimelineFile):       "{}",
	}

	for at, content := range written {
		if err := os.WriteFile(at, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", at, err)
		}
	}

	if err := runs.Retire(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("retire the run: %v", err)
	}

	for at := range written {
		_, err := os.Stat(at)
		gone := os.IsNotExist(err)

		keeping := strings.Contains(at, entity.ExecutionTaskFile) ||
			strings.Contains(at, entity.RunTimelineFile)

		if keeping && gone {
			t.Fatalf(
				"%s was removed with the workspace, so the run can no longer say what it was or "+
					"what happened",
				at,
			)
		}

		if !keeping && !gone {
			t.Fatalf("%s survived retirement, so the disk was never really given back", at)
		}
	}
}

func TestWhenARunSettledIsWrittenDownWithIt(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	settled := time.Date(2026, 8, 23, 9, 30, 0, 0, time.UTC)

	if err := runs.SaveTask(ctx, entity.Execution{
		ID:         "exec-01ABC",
		Reference:  "NORN-55",
		State:      channelv1.StateCompleted,
		AcceptedAt: settled.Add(-time.Hour),
		SettledAt:  settled,
	}); err != nil {
		t.Fatalf("write the task: %v", err)
	}

	held, err := runs.LoadTask(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("read the task back: %v", err)
	}

	if !held.SettledAt.Equal(settled) {
		t.Fatalf(
			"settledAt = %s, want %s. Without it a machine that restarts has no deadline to keep "+
				"the workspace to and would hold it forever",
			held.SettledAt, settled,
		)
	}
}

func TestALongLineOnARunsTimelineDoesNotTakeTheRestOfItWithIt(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	long := entity.TimelineEntry{
		Kind:     channelv1.EventNote,
		Reason:   strings.Repeat("why it failed. ", 8000),
		Occurred: time.Now().UTC(),
	}

	for _, entry := range []entity.TimelineEntry{long, {
		Kind:     channelv1.EventNote,
		Reason:   "and then it stopped",
		Occurred: time.Now().UTC(),
	}} {
		if err := runs.Append(ctx, "exec-01ABC", entry); err != nil {
			t.Fatalf("append a timeline entry: %v", err)
		}
	}

	timeline, err := runs.Timeline(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf(
			"one long line made the whole timeline unreadable, and the run that wrote one is the "+
				"run somebody most needs to read: %v",
			err,
		)
	}

	if len(timeline) != 2 || timeline[1].Reason != "and then it stopped" {
		t.Fatalf("timeline came back as %d entries: %+v", len(timeline), timeline)
	}
}
