package entity_test

import (
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestABranchNamesTheIssueTheRepositoryAndTheAttempt(t *testing.T) {
	cases := []struct {
		name       string
		issueKey   string
		repository string
		attempt    int
		want       string
	}{
		{"a first attempt carries no suffix", "NORN-46", "runner", 1, "norn/NORN-46/runner"},
		{"an unnumbered attempt is the first", "NORN-46", "runner", 0, "norn/NORN-46/runner"},
		{"a second attempt is r2", "NORN-46", "runner", 2, "norn/NORN-46/runner-r2"},
		{"a third attempt is r3", "NORN-46", "runner", 3, "norn/NORN-46/runner-r3"},
		{"a space becomes a dash", "NORN-46", "web ui", 1, "norn/NORN-46/web-ui"},
		{"characters git forbids become dashes", "NORN-46", "a~b^c:d?e*f[g", 1, "norn/NORN-46/a-b-c-d-e-f-g"},
		{"a double dot is collapsed", "NORN-46", "a..b", 1, "norn/NORN-46/a.b"},
		{"a lock suffix is dropped", "NORN-46", "cache.lock", 1, "norn/NORN-46/cache"},
		{"a name of nothing usable still names something", "NORN-46", "///", 1, "norn/NORN-46/repository"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := entity.BranchFor(test.issueKey, test.repository, test.attempt)
			if got != test.want {
				t.Fatalf(
					"the branch for %q in %q at attempt %d is %q and should be %q; a branch git "+
						"refuses is a snapshot that cannot be pushed",
					test.issueKey, test.repository, test.attempt, got, test.want,
				)
			}
		})
	}
}

func TestTwoAttemptsAtOneIssueNeverShareABranch(t *testing.T) {
	first := entity.BranchFor("NORN-46", "runner", 1)
	second := entity.BranchFor("NORN-46", "runner", 2)

	if first == second {
		t.Fatalf(
			"attempt 1 and attempt 2 both became %q; a new execution must not commit onto the "+
				"branch a previous one left behind",
			first,
		)
	}
}

func TestTwoRepositoriesInOneCodebaseNeverShareABranch(t *testing.T) {
	one := entity.BranchFor("NORN-46", "norn", 1)
	other := entity.BranchFor("NORN-46", "wt", 1)

	if one == other {
		t.Fatalf(
			"two repositories both became %q; a linked worktree shares its object store with the "+
				"repository it came from, so a shared name would be a collision",
			one,
		)
	}
}

func TestARunIsNamedAfterTheIssueAndTheAttempt(t *testing.T) {
	if got := entity.RunNameFor("NORN-46", 1); got != "snap-NORN-46-1" {
		t.Fatalf("a run for NORN-46 is named %q", got)
	}

	if got := entity.RunNameFor("NORN-46", 2); got != "snap-NORN-46-2" {
		t.Fatalf("a second run for NORN-46 is named %q", got)
	}
}

func TestTheProvisionalCommitSaysWhatItStartedFrom(t *testing.T) {
	message := entity.LocalChangesMessage("3a91c2f8b7d64e05aa11")

	if message != "norn: local changes at 3a91c2f8b7d6" {
		t.Fatalf(
			"the provisional commit reads %q; it has to name the commit the changes were taken "+
				"from, or a reviewer cannot tell what the coding agent started with",
			message,
		)
	}
}
