package entity_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestOnlyTheProfilesAndDriversNornKnowsAboutAreAccepted(t *testing.T) {
	for _, profile := range entity.PermissionProfiles() {
		if !profile.Valid() {
			t.Fatalf("%q is listed as a profile but refuses itself", profile)
		}
	}

	for _, made := range []entity.PermissionProfile{"", "loose", "Standard"} {
		if made.Valid() {
			t.Fatalf("%q was accepted as a permission profile", made)
		}
	}

	for _, driver := range entity.DriverKinds() {
		if !driver.Valid() {
			t.Fatalf("%q is listed as a coding agent but refuses itself", driver)
		}
	}

	for _, made := range []entity.DriverKind{"", "claude-code", "Codex"} {
		if made.Valid() {
			t.Fatalf("%q was accepted as a coding agent", made)
		}
	}
}

func TestAFailureNamesTheStepItHappenedOnAndKeepsTheCause(t *testing.T) {
	said := entity.Failure(
		entity.StepSnapshot,
		errors.New("the local changes do not apply onto the base commit: runner"),
	)

	if !strings.Contains(said, string(entity.StepSnapshot)) {
		t.Fatalf("the failure does not say what was being done: %q", said)
	}

	if !strings.Contains(said, "do not apply onto the base commit: runner") {
		t.Fatalf("the failure lost the cause a person would act on: %q", said)
	}

	if !strings.HasPrefix(said, "this machine could not ") {
		t.Fatalf("the failure does not read as a sentence: %q", said)
	}
}
