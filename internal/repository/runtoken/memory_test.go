package runtoken_test

import (
	"context"
	"testing"

	"github.com/usenorn/runner/internal/repository/runtoken"
)

func TestARunIsGivenOneTokenAndKeepsIt(t *testing.T) {
	tokens := runtoken.New()
	ctx := context.Background()

	minted, err := tokens.Mint(ctx, "exec-01ONE")
	if err != nil {
		t.Fatalf("mint a token: %v", err)
	}

	if minted == "" {
		t.Fatalf("a run was given an empty token, which every caller would then present")
	}

	again, err := tokens.Mint(ctx, "exec-01ONE")
	if err != nil {
		t.Fatalf("mint a token again: %v", err)
	}

	if again != minted {
		t.Fatalf(
			"the same run was given a second token, so a session started before the change " +
				"would be locked out of its own run halfway through",
		)
	}
}

func TestTwoRunsAreGivenDifferentTokensAndNeitherOpensTheOther(t *testing.T) {
	tokens := runtoken.New()
	ctx := context.Background()

	mine, err := tokens.Mint(ctx, "exec-01MINE")
	if err != nil {
		t.Fatalf("mint a token: %v", err)
	}

	yours, err := tokens.Mint(ctx, "exec-01YOURS")
	if err != nil {
		t.Fatalf("mint a token: %v", err)
	}

	if mine == yours {
		t.Fatalf("two runs were given the same token, so either could act as the other")
	}

	if tokens.Allows(ctx, "exec-01YOURS", mine) {
		t.Fatalf(
			"one run's token opened another run. That is the whole of what scoping a tool call " +
				"to an execution means",
		)
	}

	if !tokens.Allows(ctx, "exec-01MINE", mine) {
		t.Fatalf("a run's own token did not open it, so nothing inside it could act at all")
	}
}

func TestAnEmptyTokenOpensNothing(t *testing.T) {
	tokens := runtoken.New()
	ctx := context.Background()

	if _, err := tokens.Mint(ctx, "exec-01EMPTY"); err != nil {
		t.Fatalf("mint a token: %v", err)
	}

	if tokens.Allows(ctx, "exec-01EMPTY", "") {
		t.Fatalf("presenting nothing was taken as presenting the right thing")
	}

	if tokens.Allows(ctx, "exec-01UNKNOWN", "") {
		t.Fatalf("a run nobody minted a token for was opened by presenting nothing")
	}
}

func TestATokenIsWorthNothingOnceItsRunIsOver(t *testing.T) {
	tokens := runtoken.New()
	ctx := context.Background()

	minted, err := tokens.Mint(ctx, "exec-01OVER")
	if err != nil {
		t.Fatalf("mint a token: %v", err)
	}

	tokens.Release(ctx, "exec-01OVER")

	if tokens.Allows(ctx, "exec-01OVER", minted) {
		t.Fatalf(
			"a token outlived the run it was minted for. Anything that kept a copy could still " +
				"act on a run nobody is watching any more",
		)
	}
}
