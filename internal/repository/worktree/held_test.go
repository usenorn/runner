package worktree

import (
	"errors"
	"testing"
)

func TestABranchAnotherWorktreeHoldsIsRecognisedWhicheverGitSaysSo(t *testing.T) {
	for name, said := range map[string]string{
		"git 2.42 and later": "fatal: 'norn/NORN-47/runner' is already used by worktree at '/runs/exec-1/workspace/runner'",
		"older git":          "fatal: 'norn/NORN-47/runner' is already checked out at '/runs/exec-1/workspace/runner'",
	} {
		t.Run(name, func(t *testing.T) {
			if !heldElsewhere(errors.New(said)) {
				t.Fatalf("%q was not recognised, so the person sees raw git output instead of what to do", said)
			}
		})
	}

	if heldElsewhere(errors.New("fatal: invalid reference: norn/NORN-47/runner")) {
		t.Fatal("an unrelated git failure was read as a branch held elsewhere")
	}
}
