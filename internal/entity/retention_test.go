package entity_test

import (
	"slices"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func at(minutes int) time.Time {
	return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
}

func TestAFinishedRunKeepsItsWorkspaceUntilTheWindowHasPassed(t *testing.T) {
	runs := []entity.RunUsage{
		{Name: "young", Settled: at(-10), Finished: true, Workspace: true},
		{Name: "old", Settled: at(-40), Finished: true, Workspace: true},
	}

	retirable := entity.Retirable(runs, at(0), 30*time.Minute)

	if !slices.Equal(retirable, []string{"old"}) {
		t.Fatalf(
			"retirable = %v, want only old. A run given back before its window is a workspace "+
				"somebody was still looking at",
			retirable,
		)
	}
}

func TestARunWithNothingLeftToGiveBackIsNotRetiredAgain(t *testing.T) {
	runs := []entity.RunUsage{
		{Name: "done", Settled: at(-90), Finished: true, Workspace: false},
	}

	if retirable := entity.Retirable(runs, at(0), 30*time.Minute); len(retirable) != 0 {
		t.Fatalf(
			"retirable = %v, want none. Retiring a run whose workspace is already gone would put "+
				"the same line on its timeline every sweep",
			retirable,
		)
	}
}

func TestARunStillUnderWayIsNeverGivenBackOrTakenOffTheDisk(t *testing.T) {
	runs := []entity.RunUsage{
		{Name: "live", Bytes: 90, Settled: at(-1000), Finished: true, Held: true, Workspace: true},
		{Name: "working", Bytes: 90, Finished: false, Workspace: true},
	}

	if retirable := entity.Retirable(runs, at(0), time.Minute); len(retirable) != 0 {
		t.Fatalf("retirable = %v, want none. That run is still leased to norn", retirable)
	}

	if reapable := entity.Reapable(runs, at(0), time.Minute, 1); len(reapable) != 0 {
		t.Fatalf(
			"reapable = %v, want none. Deleting a run this machine is still holding takes the "+
				"workspace out from under a coding agent",
			reapable,
		)
	}
}

func TestASnapshotSomebodyTookByHandIsCountedButNeverCollected(t *testing.T) {
	runs := []entity.RunUsage{
		{Name: "snap-NORN-55-1", Bytes: 500, Workspace: true},
		{Name: "exec-01ABC", Bytes: 100, Settled: at(-1), Finished: true, Workspace: true},
	}

	if held := entity.Occupied(runs); held != 600 {
		t.Fatalf(
			"occupied = %d, want 600. A snapshot taken by hand still fills the disk, so leaving "+
				"it out of the figure would understate what the machine is using",
			held,
		)
	}

	reapable := entity.Reapable(runs, at(0), time.Hour, 200)

	if !slices.Equal(reapable, []string{"exec-01ABC"}) {
		t.Fatalf(
			"reapable = %v, want only exec-01ABC. A snapshot somebody took by hand is theirs to "+
				"remove",
			reapable,
		)
	}
}

func TestRunsPastTheirAgeAreTakenOffTheDiskWhateverTheDiskHasRoomFor(t *testing.T) {
	runs := []entity.RunUsage{
		{Name: "ancient", Bytes: 1, Settled: at(-60), Finished: true},
		{Name: "recent", Bytes: 1, Settled: at(-5), Finished: true},
	}

	reapable := entity.Reapable(runs, at(0), 30*time.Minute, 1<<40)

	if !slices.Equal(reapable, []string{"ancient"}) {
		t.Fatalf("reapable = %v, want only ancient", reapable)
	}
}

func TestWhenTheDiskIsOverBudgetTheOldestRunsGoFirstAndOnlyAsManyAsItTakes(t *testing.T) {
	runs := []entity.RunUsage{
		{Name: "second", Bytes: 40, Settled: at(-20), Finished: true},
		{Name: "third", Bytes: 40, Settled: at(-10), Finished: true},
		{Name: "first", Bytes: 40, Settled: at(-30), Finished: true},
	}

	reapable := entity.Reapable(runs, at(0), time.Hour, 50)

	if !slices.Equal(reapable, []string{"first", "second"}) {
		t.Fatalf(
			"reapable = %v, want first then second. Collecting more than the budget asks for "+
				"throws away history nobody needed to lose",
			reapable,
		)
	}
}

func TestWhatTheMachineCannotCollectIsStillCounted(t *testing.T) {
	runs := []entity.RunUsage{
		{Name: "live", Bytes: 100, Finished: false},
		{Name: "gone", Bytes: 100, Settled: at(-1), Finished: true},
	}

	if left := entity.Left(runs, []string{"gone"}); left != 100 {
		t.Fatalf(
			"left = %d, want 100. The machine has to know it is still over its budget so it can "+
				"say so rather than look tidy",
			left,
		)
	}
}

func TestHowLongAMachineKeepsThingsIsSaidInWordsAPersonReads(t *testing.T) {
	for _, held := range []struct {
		span   time.Duration
		wanted string
	}{
		{14 * 24 * time.Hour, "14 days"},
		{24 * time.Hour, "24 hours"},
		{time.Hour, "60 minutes"},
		{30 * time.Minute, "30 minutes"},
		{time.Minute, "1 minute"},
		{45 * time.Second, "45s"},
	} {
		if said := entity.Span(held.span); said != held.wanted {
			t.Fatalf(
				"%s reads as %q, want %q. A person deciding whether their workspace is still "+
					"there should not have to divide hours by 24",
				held.span, said, held.wanted,
			)
		}
	}
}
