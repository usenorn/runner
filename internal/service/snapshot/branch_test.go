package snapshot_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/usenorn/runner/internal/service"
)

func (h *harness) takeOn(branch string, attempt int) error {
	h.t.Helper()

	_, err := h.service.Take(context.Background(), service.TakeRequest{
		Path:     h.root,
		IssueKey: "NORN-46",
		Attempt:  attempt,
		Branch:   branch,
	})

	return err
}

func TestARunWorksOnTheBranchNornGaveTheIssue(t *testing.T) {
	h := newHarness(t, defaults())

	if err := h.takeOn("rae/norn-46-snapshot-engine", 1); err != nil {
		t.Fatalf("take a snapshot: %v", err)
	}

	if !slices.Contains(h.branched, "rae/norn-46-snapshot-engine") {
		t.Fatalf(
			"the run branched %v, and none of them is the name norn gave the issue. A person "+
				"looking for the work would not find it where norn told them it would be",
			h.branched,
		)
	}
}

func TestAnEarlierAttemptsBranchStillWinsSoItsPullRequestIsAmended(t *testing.T) {
	h := newHarness(t, defaults())

	_, err := h.service.Take(context.Background(), service.TakeRequest{
		Path:     h.root,
		IssueKey: "NORN-46",
		Attempt:  2,
		Branch:   "rae/norn-46-snapshot-engine",
		Branches: map[string]string{"runner": "rae/norn-46-first-go"},
	})
	if err != nil {
		t.Fatalf("take a snapshot: %v", err)
	}

	if !slices.Contains(h.branched, "rae/norn-46-first-go") {
		t.Fatalf(
			"a second attempt branched %v instead of carrying on where the first one left off, "+
				"so it would open a second pull request beside the one being reviewed",
			h.branched,
		)
	}
}

func TestARunWithNoBranchFromNornStillGetsOne(t *testing.T) {
	h := newHarness(t, defaults())

	if err := h.takeOn("", 1); err != nil {
		t.Fatalf("take a snapshot: %v", err)
	}

	if !slices.Contains(h.branched, "norn/NORN-46/runner") {
		t.Fatalf(
			"a run whose offer named no branch branched %v. Naming a branch must never be the "+
				"thing that stops a run",
			h.branched,
		)
	}
}

func TestTheRemoteTipIsRecordedBeforeTheAgentCanTouchAnything(t *testing.T) {
	h := newHarness(t, defaults())
	h.remote = "git@github.com:usenorn/norn.git"
	h.remoteTip = "7c5d2e1b9a8f7c6d5e4b3a298f2a1c9d4b6e0a3f"

	taken, err := h.service.Take(context.Background(), service.TakeRequest{
		Path: h.root, IssueKey: "NORN-231", Attempt: 1, Branch: "rae/norn-231",
	})
	if err != nil {
		t.Fatalf("take a snapshot: %v", err)
	}

	held := taken.Repositories[0]
	if held.Remote != h.remote || !held.Lease.Known || held.Lease.Tip != h.remoteTip {
		t.Fatalf(
			"the snapshot recorded remote %q and lease %+v; publishing must overwrite only the "+
				"branch as it stood before the agent ran, never whatever a person pushed since",
			held.Remote, held.Lease,
		)
	}

	if held.GitDir == "" {
		t.Fatal("the snapshot has no git dir, so nothing can be kept out of the agent's reach")
	}
}

func TestARemoteThatCannotBeAskedLeavesTheLeaseUnknownAndSaysSo(t *testing.T) {
	h := newHarness(t, defaults())
	h.remote = "git@github.com:usenorn/norn.git"
	h.tipFails = errors.New("could not resolve host")

	taken, err := h.service.Take(context.Background(), service.TakeRequest{
		Path: h.root, IssueKey: "NORN-231", Attempt: 1, Branch: "rae/norn-231",
	})
	if err != nil {
		t.Fatalf("a remote that cannot be reached should not stop the run: %v", err)
	}

	if taken.Repositories[0].Lease.Known {
		t.Fatal("a lease nobody could read was recorded as known")
	}

	if len(taken.Warnings) == 0 {
		t.Fatal("the run never said its branch will only be fast-forwarded")
	}
}
