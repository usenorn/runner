package entity_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestProgressOutsideAPercentageIsRefused(t *testing.T) {
	for _, percent := range []int{-1, 101, 1000} {
		err := entity.Progress{Summary: "working", Percent: percent}.Valid()

		if !errors.Is(err, entity.ErrProgressRange) {
			t.Fatalf("a run reported itself %d%% done and nothing stopped it: %v", percent, err)
		}
	}
}

func TestAProgressLineNamesItsPhaseWhenThereIsOne(t *testing.T) {
	with := entity.Progress{Summary: "the tests are running", Phase: "testing"}.Line()

	if !strings.HasPrefix(with, "testing: ") {
		t.Fatalf("a line with a phase reads %q, so somebody following it cannot skim", with)
	}

	without := entity.Progress{Summary: "the tests are running"}.Line()

	if without != "the tests are running" {
		t.Fatalf("a line with no phase reads %q, with something invented in front of it", without)
	}
}

func TestFinishingWithNothingToSayIsRefusedAndNotesRideAlongWhenThereAreSome(t *testing.T) {
	if err := (entity.Completion{Summary: "  "}).Valid(); !errors.Is(
		err, entity.ErrCompleteEmpty,
	) {
		t.Fatalf("a run was finished with a blank summary: %v", err)
	}

	said := entity.Completion{
		Summary: "added a median helper",
		Notes:   "the convention was decided by a person",
	}.Line()

	for _, wanted := range []string{"added a median helper", "decided by a person"} {
		if !strings.Contains(said, wanted) {
			t.Fatalf("what a reviewer reads is %q, and it drops %q", said, wanted)
		}
	}
}
