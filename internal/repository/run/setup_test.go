package run_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	runrepo "github.com/usenorn/runner/internal/repository/run"
)

func TestOpeningARunAgainTakesItAsItStandsRatherThanRefusingIt(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	first, err := runs.Open(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("open a run: %v", err)
	}

	again, err := runs.Open(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf(
			"opening a run a second time returned %v; a start norn sends twice must not lose the "+
				"work the first one did",
			err,
		)
	}

	if first != again {
		t.Fatalf("the same run opened as %q and then %q", first, again)
	}
}

func TestGivingTheWorkspaceBackLeavesBehindEverythingThatExplainsTheRun(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	run, err := runs.Open(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("open a run: %v", err)
	}

	if err := runs.Save(ctx, snapshot("exec-01ABC", run)); err != nil {
		t.Fatalf("save the snapshot: %v", err)
	}

	if err := runs.Append(ctx, "exec-01ABC", entity.TimelineEntry{
		Kind: channelv1.EventPhase, Reason: "something happened", Occurred: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write a timeline entry: %v", err)
	}

	if err := runs.Prune(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("prune the run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(run, entity.RunWorkspaceDir)); !os.IsNotExist(err) {
		t.Fatalf("the workspace is still there: %v", err)
	}

	if _, err := runs.Load(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("the record of what the run copied went with the workspace: %v", err)
	}

	timeline, err := runs.Timeline(ctx, "exec-01ABC")
	if err != nil || len(timeline) != 1 {
		t.Fatalf("the run's own timeline came back as %+v (%v)", timeline, err)
	}
}

func TestWhatARunWasSetUpWithComesBackExactlyAsItWasWrittenDown(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	want := entity.RunSetup{
		Permissions: entity.RunPermissions{
			Profile: entity.ProfileStandard, Chosen: "the machine's default",
		},
		Plan: entity.RunPlan{Source: entity.PlanCodebase, Path: "/codebase/.norn/run-plan.yaml"},
		Driver: entity.RunDriver{
			Kind:      entity.DriverClaude,
			Version:   "2.0.1",
			Installed: true,
			Model:     "claude-sonnet-5",
			Chosen:    "the delegation asked for it",
		},
		Services: entity.RunServices{
			Runtime: entity.RuntimeProcess, Chosen: "nothing asked for anything else",
		},
	}

	if err := runs.SaveSetup(ctx, "exec-01ABC", want); err != nil {
		t.Fatalf("write what the run is set up with: %v", err)
	}

	for _, file := range []string{
		entity.RunPermissionsFile,
		entity.RunPlanFile,
		entity.RunDriverFile,
		entity.RunServicesFile,
	} {
		path := filepath.Join(dir.Run("exec-01ABC"), entity.RunMetadataDir, file)

		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s is missing from the run directory: %v", file, err)
		}
	}

	got, err := runs.LoadSetup(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("read what the run is set up with: %v", err)
	}

	if got.Permissions != want.Permissions || got.Plan != want.Plan {
		t.Fatalf("the run came back set up as %+v", got)
	}

	if !reflect.DeepEqual(got.Driver, want.Driver) {
		t.Fatalf("the run came back set up for %+v", got.Driver)
	}

	if got.Services.Runtime != want.Services.Runtime || got.Services.Chosen != want.Services.Chosen {
		t.Fatalf("the run came back running services on %+v", got.Services)
	}
}

func TestARunsOwnTimelineReadsBackInTheOrderThingsHappened(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	at := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)

	for index, reason := range []string{"first", "second", "third"} {
		if err := runs.Append(ctx, "exec-01ABC", entity.TimelineEntry{
			Kind:     channelv1.EventPhase,
			State:    channelv1.StatePreparing,
			Reason:   reason,
			Occurred: at.Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatalf("write a timeline entry: %v", err)
		}
	}

	timeline, err := runs.Timeline(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("read the timeline: %v", err)
	}

	if len(timeline) != 3 {
		t.Fatalf("the timeline came back with %d entries", len(timeline))
	}

	for index, reason := range []string{"first", "second", "third"} {
		if timeline[index].Reason != reason {
			t.Fatalf("entry %d reads %q, want %q", index, timeline[index].Reason, reason)
		}
	}

	if timeline[0].State != channelv1.StatePreparing || timeline[0].Kind != channelv1.EventPhase {
		t.Fatalf("the first entry came back as %+v", timeline[0])
	}
}

func TestAskingForTheTimelineOfARunThisMachineNeverHadSaysSo(t *testing.T) {
	dir, ctx := store(t)

	if _, err := runrepo.New(dir).Timeline(ctx, "exec-01GONE"); err == nil {
		t.Fatalf("a run this machine never had answered with a timeline")
	}
}

func TestTheSessionsARunHasDrivenAreWrittenDownAndComeBackInOrder(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	began := time.Date(2026, 8, 22, 19, 0, 0, 0, time.UTC)

	want := entity.RunDriver{
		Kind:      entity.DriverClaude,
		Version:   "2.1.239",
		Installed: true,
		Model:     "opus",
		Chosen:    "the delegation asked for it",
		Resumes:   1,
		Sessions: []entity.DriverSession{
			{
				ID:        "session-01",
				StartedAt: began,
				EndedAt:   began.Add(time.Minute),
				Outcome:   entity.OutcomeCrashed,
			},
			{
				ID:        "session-01",
				StartedAt: began.Add(2 * time.Minute),
				EndedAt:   began.Add(5 * time.Minute),
				Outcome:   entity.OutcomeDone,
				Reason:    "the work is committed",
			},
		},
	}

	if err := runs.SaveDriver(ctx, "exec-01ABC", want); err != nil {
		t.Fatalf("write down what the run was driven with: %v", err)
	}

	got, err := runs.LoadDriver(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("read what the run was driven with: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the run came back driven by %+v", got)
	}

	held, found := got.Latest()

	if !found || held.Outcome != entity.OutcomeDone {
		t.Fatalf("the run carries on from %+v", held)
	}
}
