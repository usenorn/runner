package entity_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestOnlyARefusalStopsTheRunnerFromTryingNorn(t *testing.T) {
	settled := map[entity.ChannelState]bool{
		entity.ChannelOff:        false,
		entity.ChannelConnecting: false,
		entity.ChannelLive:       false,
		entity.ChannelOffline:    false,
		entity.ChannelRefused:    true,
	}

	for state, want := range settled {
		if !state.Valid() {
			t.Errorf("%q is not one of the states a channel may be in", state)
		}

		if state.Settled() != want {
			t.Errorf("%q settled=%v, want %v", state, state.Settled(), want)
		}
	}
}

func TestNornRefusingThisBuildIsNotSomethingToRetry(t *testing.T) {
	cases := map[error]entity.ChannelState{
		entity.ErrRunnerOutdated:    entity.ChannelRefused,
		entity.ErrRunnerRevoked:     entity.ChannelRefused,
		entity.ErrAgentDisabled:     entity.ChannelRefused,
		entity.ErrChannelOff:        entity.ChannelRefused,
		entity.ErrNotEnrolled:       entity.ChannelOff,
		entity.ErrServerUnreachable: entity.ChannelOffline,
		entity.ErrChannelDisplaced:  entity.ChannelOffline,
		entity.ErrChannelClosed:     entity.ChannelOffline,
	}

	for err, want := range cases {
		if got := entity.ChannelStateFor(err); got != want {
			t.Errorf("%v left the channel %q, want %q", err, got, want)
		}
	}
}

func TestAnOutdatedRunnerCarriesNornsOwnWordsToTheUser(t *testing.T) {
	detail := "this runner is 0.9.0 and norn needs 1.2.0 or newer"
	err := error(entity.OutdatedError{Detail: detail})

	if !errors.Is(err, entity.ErrRunnerOutdated) {
		t.Fatalf("an outdated build is not recognised as one")
	}

	if err.Error() != detail {
		t.Fatalf("the refusal reads %q, want norn's own words", err.Error())
	}

	if bare := error(entity.OutdatedError{}); bare.Error() != entity.ErrRunnerOutdated.Error() {
		t.Fatalf("a refusal with nothing to say reads %q", bare.Error())
	}
}

func TestEveryDeclineSaysWhyInWordsAPersonCanActOn(t *testing.T) {
	report := entity.SchedulerReport{
		Capacity: 2,
		Used:     2,
		Room:     entity.Room{Free: 1 << 30, Watermark: 10 << 30, Known: true},
	}

	for _, reason := range entity.DeclineReasons() {
		if !reason.Valid() {
			t.Errorf("%q is not one of the reasons work is turned down", reason)
		}

		because := reason.Because(report)

		if because == "" || because == string(reason) {
			t.Errorf("%q explains itself as %q", reason, because)
		}
	}

	room := entity.DeclineDiskPressure.Because(report)

	if !strings.Contains(room, "1.0 GB") || !strings.Contains(room, "10.0 GB") {
		t.Fatalf("the disk decline reads %q and names neither figure", room)
	}
}

func TestARunnerTurnsWorkDownForTheMostPressingReasonFirst(t *testing.T) {
	pressed := entity.Room{Free: 1, Watermark: 2, Known: true}

	cases := []struct {
		report entity.SchedulerReport
		reason entity.DeclineReason
		turned bool
	}{
		{report: entity.SchedulerReport{Capacity: 2, Used: 0}, turned: false},
		{
			report: entity.SchedulerReport{Capacity: 2, Used: 2},
			reason: entity.DeclineAtCapacity,
			turned: true,
		},
		{
			report: entity.SchedulerReport{Capacity: 2, Used: 0, Room: pressed},
			reason: entity.DeclineDiskPressure,
			turned: true,
		},
		{
			report: entity.SchedulerReport{Capacity: 2, Used: 2, Paused: true, Room: pressed},
			reason: entity.DeclinePaused,
			turned: true,
		},
	}

	for _, want := range cases {
		reason, turned := want.report.Decline()

		if turned != want.turned || reason != want.reason {
			t.Errorf(
				"%+v declined=%v for %q, want declined=%v for %q",
				want.report, turned, reason, want.turned, want.reason,
			)
		}
	}
}

func TestRoomIsOnlyPressedWhenTheRunnerActuallyKnowsHowMuchIsLeft(t *testing.T) {
	cases := map[string]struct {
		room    entity.Room
		pressed bool
	}{
		"below the watermark":        {room: entity.Room{Free: 1, Watermark: 2, Known: true}, pressed: true},
		"above the watermark":        {room: entity.Room{Free: 3, Watermark: 2, Known: true}},
		"nothing kept back":          {room: entity.Room{Free: 0, Watermark: 0, Known: true}},
		"the disk could not be read": {room: entity.Room{Watermark: 2}},
	}

	for name, want := range cases {
		if got := want.room.Pressed(); got != want.pressed {
			t.Errorf("%s: pressed=%v, want %v", name, got, want.pressed)
		}
	}
}

func TestBytesAreWrittenInTheUnitAPersonWouldUse(t *testing.T) {
	cases := map[int64]string{
		0:        "0 B",
		512:      "512 B",
		1024:     "1.0 KB",
		1536:     "1.5 KB",
		1 << 20:  "1.0 MB",
		10 << 30: "10.0 GB",
		3 << 40:  "3.0 TB",
	}

	for bytes, want := range cases {
		if got := entity.ByteSize(bytes); got != want {
			t.Errorf("%d reads as %q, want %q", bytes, got, want)
		}
	}

	if got := entity.ByteSize(4 << 50); !strings.HasSuffix(got, "TB") {
		t.Errorf("%q should still be counted in terabytes", got)
	}
}
