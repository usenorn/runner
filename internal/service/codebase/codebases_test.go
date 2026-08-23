package codebase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestConnectingAFolderRegistersItAndRecordsWhatThePersonAgreedTo(t *testing.T) {
	h := newHarness(t)

	h.connect()

	if !h.norn.connected {
		t.Fatalf("norn was never told about the folder")
	}

	recorded := h.recorded()

	if len(recorded.Confirmed.Listed()) != 2 {
		t.Fatalf("the record holds %d repositories, want the two that were scanned",
			len(recorded.Confirmed.Listed()))
	}

	if recorded.Drifted() {
		t.Fatalf("a folder reads as drifted the moment it is connected")
	}
}

func TestAFolderWithNoRepositoriesIsNotConnected(t *testing.T) {
	h := newHarness(t)
	h.finds = nil

	if _, err := h.service.Scan(context.Background(), root); !errors.Is(err, entity.ErrCodebaseEmpty) {
		t.Fatalf(
			"a folder with no repositories was accepted and got %v; there would be nothing for an "+
				"agent to work on",
			err,
		)
	}
}

func TestAFolderThatOverlapsOneAlreadyConnectedIsRefused(t *testing.T) {
	h := newHarness(t)

	h.connect()

	_, err := h.service.Scan(context.Background(), root+"/norn")
	if !errors.Is(err, entity.ErrCodebaseOverlaps) {
		t.Fatalf(
			"a folder inside a connected one was accepted and got %v; the same repository would "+
				"be offered from two codebases",
			err,
		)
	}
}

func TestAnUnchangedFolderIsNotReportedAgain(t *testing.T) {
	h := newHarness(t)

	h.connect()

	connects := h.norn.connects
	h.rescan()

	if h.norn.connects != connects {
		t.Fatalf("a folder that had not changed was reported to norn again")
	}
}

func TestAddingARepositoryPutsTheFolderInDriftAndWaitsForAPerson(t *testing.T) {
	h := newHarness(t)

	h.connect()

	h.finds = append(h.finds, "landing")
	h.rescan()

	if !h.norn.drifted {
		t.Fatalf("a repository appeared and norn was never told the folder had drifted")
	}

	if !h.recorded().Drifted() {
		t.Fatalf("the machine does not read as drifted while norn does")
	}

	if len(h.recorded().Confirmed.Listed()) != 2 {
		t.Fatalf(
			"the confirmed inventory grew to %d without anybody confirming it",
			len(h.recorded().Confirmed.Listed()),
		)
	}
}

func TestTheRegularRescanNeverQuietlyClearsDriftNobodyConfirmed(t *testing.T) {
	h := newHarness(t)

	h.connect()

	h.finds = append(h.finds, "landing")
	h.rescan()
	h.rescan()
	h.rescan()

	if !h.norn.drifted {
		t.Fatalf(
			"drift cleared itself after a few rescans; norn compares what it is sent against what " +
				"it already holds, so re-sending an unconfirmed set would read as agreement and " +
				"a person would never be asked",
		)
	}

	if h.norn.confirms != 0 {
		t.Fatalf("the machine confirmed drift %d times on its own", h.norn.confirms)
	}
}

func TestAFolderThatGoesBackToWhatWasConfirmedStopsBeingDrifted(t *testing.T) {
	h := newHarness(t)

	h.connect()

	h.finds = append(h.finds, "landing")
	h.rescan()

	h.finds = h.finds[:2]
	h.rescan()

	if h.norn.drifted {
		t.Fatalf(
			"a folder holding exactly what was confirmed is still drifted in norn; that set was " +
				"already agreed to",
		)
	}

	if h.recorded().Drifted() {
		t.Fatalf("the machine still reads as drifted after the folder went back")
	}

	if h.norn.confirms != 1 {
		t.Fatalf(
			"norn was confirmed %d times, want once; going back to the confirmed set has to be "+
				"told to norn, not just noticed locally",
			h.norn.confirms,
		)
	}
}

func TestConfirmingDriftAcceptsTheFolderAsItNowStands(t *testing.T) {
	h := newHarness(t)

	h.connect()

	h.finds = append(h.finds, "landing")
	h.rescan()

	scan := h.scan()

	if !scan.Drift.Any() {
		t.Fatalf("the scan does not report the repository that was added")
	}

	if _, err := h.service.Accept(context.Background(), scan); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	if h.norn.drifted {
		t.Fatalf("norn still holds the folder as drifted after it was confirmed")
	}

	if len(h.recorded().Confirmed.Listed()) != 3 {
		t.Fatalf(
			"the confirmed inventory holds %d repositories, want the three that are there now",
			len(h.recorded().Confirmed.Listed()),
		)
	}
}

func TestAFolderThatIsNoLongerThereIsLeftAloneRatherThanDisconnected(t *testing.T) {
	h := newHarness(t)

	h.connect()

	before := h.recorded()
	connects := h.norn.connects

	h.failing = entity.ErrCodebaseRootMissing
	h.rescan()

	if h.norn.connects != connects {
		t.Fatalf("a folder that had vanished was still reported to norn")
	}

	if len(h.recorded().Confirmed.Listed()) != len(before.Confirmed.Listed()) {
		t.Fatalf(
			"a folder that vanished lost its record; an unplugged drive must not delete what the " +
				"machine knows about the code on it",
		)
	}
}

func TestAScanOfAConnectedFolderSaysWhichCodebaseItIs(t *testing.T) {
	h := newHarness(t)

	h.connect()

	scan := h.scan()

	if !scan.Connected || scan.CodebaseID != h.norn.id {
		t.Fatalf("a scan of a connected folder came back as %+v", scan)
	}

	if scan.Drift.Any() || scan.Reconcile {
		t.Fatalf("a scan of an unchanged connected folder reports something to do: %+v", scan)
	}
}

func TestConnectingAFolderSaysWhetherThisMachineCanCarryAPreview(t *testing.T) {
	h := newHarness(t)
	h.previews = entity.PreviewService{
		Gateway: "https://tunnel.norn.ink",
		Domain:  "norn.ink",
		Scheme:  "https",
	}

	h.connect()

	if h.probed != "https://tunnel.norn.ink" {
		t.Fatalf(
			"the scan probed %q; a machine that never tries the gateway cannot tell anybody "+
				"why a preview link does not open",
			h.probed,
		)
	}

	if h.norn.gateway != entity.GatewayReachable {
		t.Fatalf(
			"norn was told the gateway is %q, want %q; the answer has to travel with the "+
				"folder or nobody sees it",
			h.norn.gateway, entity.GatewayReachable,
		)
	}
}

func TestAnInstanceServingNoPreviewDomainIsNotCalledUnreachable(t *testing.T) {
	h := newHarness(t)

	h.connect()

	if h.norn.gateway != entity.GatewayUnconfigured {
		t.Fatalf(
			"norn was told the gateway is %q, want %q; nothing to reach is not the same as "+
				"something this machine cannot reach, and reading it as a fault would send "+
				"people looking for a network problem that is not there",
			h.norn.gateway, entity.GatewayUnconfigured,
		)
	}
}
