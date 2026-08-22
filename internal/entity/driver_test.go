package entity_test

import (
	"errors"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func TestOnlyTheEventsAndOutcomesThisMachineKnowsAreValid(t *testing.T) {
	for _, kind := range entity.DriverEventKinds() {
		if !kind.Valid() {
			t.Fatalf("%s is in the list and is not valid", kind)
		}
	}

	if entity.DriverEventKind("thinking").Valid() {
		t.Fatalf("an event kind this machine does not send was taken as valid")
	}

	for _, outcome := range entity.DriverOutcomes() {
		if !outcome.Valid() {
			t.Fatalf("%s is in the list and is not valid", outcome)
		}
	}

	if entity.DriverOutcome("cancelled").Valid() {
		t.Fatalf("an outcome no driver reports was taken as valid")
	}
}

func TestOnlyASessionThatSaidItWasDoneCountsAsFinished(t *testing.T) {
	for outcome, finished := range map[entity.DriverOutcome]bool{
		entity.OutcomeDone:       true,
		entity.OutcomeFailed:     false,
		entity.OutcomeCrashed:    false,
		entity.OutcomeNeedsInput: false,
	} {
		if outcome.Finished() != finished {
			t.Fatalf("a %s session reads as finished=%t", outcome, outcome.Finished())
		}
	}
}

func TestAWorkspaceOnMinimalKeepsItsLogsAndNotItsTranscript(t *testing.T) {
	if !entity.TelemetryMinimal.Keeps(entity.StreamLogs) {
		t.Fatalf("a workspace on minimal was taken to refuse its own logs")
	}

	if entity.TelemetryMinimal.Keeps(entity.StreamTranscript) {
		t.Fatalf("a workspace on minimal was taken to keep full transcripts")
	}

	for _, stream := range entity.UploadStreams() {
		if !entity.TelemetryFull.Keeps(stream) {
			t.Fatalf("a workspace keeping everything was taken to refuse its %s", stream)
		}
	}
}

func TestAnAgentIsOnlyReadyWhenItIsBothInstalledAndSignedIn(t *testing.T) {
	for health, fault := range map[entity.DriverHealth]error{
		{Installed: true, SignedIn: true}:  nil,
		{Installed: true, SignedIn: false}: entity.ErrDriverSignedOut,
		{Installed: false}:                 entity.ErrDriverMissing,
	} {
		if health.Ready() != (fault == nil) {
			t.Fatalf("%+v reads as ready=%t", health, health.Ready())
		}

		if fault == nil {
			continue
		}

		if !errors.Is(health.Fault(), fault) {
			t.Fatalf("%+v is faulted as %v", health, health.Fault())
		}
	}
}

func TestTheSessionARunCarriesOnFromIsTheLastOneItStarted(t *testing.T) {
	driver := entity.RunDriver{Sessions: []entity.DriverSession{
		{ID: "first", StartedAt: time.Now().UTC()},
		{ID: "second", StartedAt: time.Now().UTC()},
	}}

	held, found := driver.Latest()

	if !found || held.ID != "second" {
		t.Fatalf("a run with two sessions carries on from %+v", held)
	}

	if _, found := (entity.RunDriver{}).Latest(); found {
		t.Fatalf("a run that never started a session found one to carry on from")
	}
}
