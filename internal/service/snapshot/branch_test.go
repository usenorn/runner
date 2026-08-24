package snapshot_test

import (
	"context"
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
