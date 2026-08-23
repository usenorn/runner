package entity_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestALogPatternKeepsOnlyWhatMatchesAndStillTakesTheLastOfThem(t *testing.T) {
	lines := []string{
		"listening on 43111", "GET /  200", "ERROR could not reach the database",
		"GET /x 200", "ERROR the migration failed",
	}

	kept, err := entity.LogQuery{Tail: 1, Grep: "^ERROR"}.Select(lines)
	if err != nil {
		t.Fatalf("filter a service's output: %v", err)
	}

	if !slices.Equal(kept, []string{"ERROR the migration failed"}) {
		t.Fatalf(
			"filtering answered %v. The tail has to be taken after the pattern, or asking for "+
				"the last error hands back the last line that happens to be one",
			kept,
		)
	}
}

func TestAPatternAsksForEverythingHeldSoTheTailIsNotTakenTwice(t *testing.T) {
	if window := (entity.LogQuery{Tail: 5, Grep: "ERROR"}).Window(); window != 0 {
		t.Fatalf(
			"a filtered read asked for %d lines. Cutting to the tail before the pattern runs "+
				"hides every match older than it",
			window,
		)
	}

	if window := (entity.LogQuery{Tail: 5}).Window(); window != 5 {
		t.Fatalf("an unfiltered read asked for %d lines rather than the 5 it wanted", window)
	}
}

func TestAPatternNothingCanCompileIsRefusedRatherThanIgnored(t *testing.T) {
	_, err := entity.LogQuery{Grep: "("}.Select([]string{"anything"})

	if !errors.Is(err, entity.ErrServiceInvalid) {
		t.Fatalf(
			"a pattern that is not one was quietly dropped: %v. The caller then reads an "+
				"unfiltered log believing it filtered",
			err,
		)
	}
}
