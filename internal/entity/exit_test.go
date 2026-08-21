package entity_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestNothingWrongExitsZero(t *testing.T) {
	if code := entity.ExitCode(nil); code != entity.ExitOK {
		t.Fatalf("no error exited %d, want %d", code, entity.ExitOK)
	}
}

func TestAnOrdinaryFailureExitsOne(t *testing.T) {
	if code := entity.ExitCode(errors.New("something broke")); code != entity.ExitFailure {
		t.Fatalf("a plain error exited %d, want %d", code, entity.ExitFailure)
	}
}

func TestAnExitCodeSurvivesBeingWrapped(t *testing.T) {
	wrapped := fmt.Errorf("while stopping: %w",
		entity.Exit(entity.ExitDrainForced, errors.New("the drain ran out of time")))

	if code := entity.ExitCode(wrapped); code != entity.ExitDrainForced {
		t.Fatalf("a wrapped exit error reported %d, want %d", code, entity.ExitDrainForced)
	}

	if wrapped.Error() == "" {
		t.Fatalf("the wrapped error lost its message")
	}
}
