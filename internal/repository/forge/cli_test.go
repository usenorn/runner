package forge_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	forgerepo "github.com/usenorn/runner/internal/repository/forge"
)

func results() config.Results {
	return config.Results{
		CreatePRs:    config.PullRequestsAuto,
		Attribution:  config.AttributionNone,
		PushTimeout:  time.Second,
		ForgeTimeout: 10 * time.Second,
		MaxDiffBytes: 1 << 20,
	}
}

func fake(t *testing.T, name, body string) repository.Forge {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("this machine cannot put a shell script on the path")
	}

	dir := t.TempDir()

	script := "#!/bin/sh\n" + body + "\n"

	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write a stand-in %s: %v", name, err)
	}

	t.Setenv("PATH", dir)

	return forgerepo.New(results())
}

func TestAToolNobodyIsSignedInToCountsAsNoToolAtAll(t *testing.T) {
	forges := fake(t, "gh", `
if [ "$1" = "auth" ]; then
  echo "You are not logged into any GitHub hosts." >&2
  exit 1
fi
exit 0
`)

	if kind, available := forges.Available(context.Background(), t.TempDir()); available {
		t.Fatalf(
			"%s counted as available while signed out; the run would push the branch, then fail "+
				"to open a pull request at the last step instead of saying so up front",
			kind,
		)
	}
}

func TestAToolThatIsSignedInIsTheOneUsed(t *testing.T) {
	forges := fake(t, "gh", "exit 0")

	kind, available := forges.Available(context.Background(), t.TempDir())
	if !available || kind != entity.ForgeGitHub {
		t.Fatalf("a signed-in gh reads as %q, %t", kind, available)
	}
}

func TestOpeningAPullRequestAnswersTheAddressAPersonOpens(t *testing.T) {
	forges := fake(t, "gh", `
if [ "$1" = "auth" ]; then exit 0; fi
if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
  echo "https://github.com/usenorn/runner/pull/231"
  exit 0
fi
exit 1
`)

	address, err := forges.Open(context.Background(), t.TempDir(), entity.PullRequest{
		Title:  "NORN-54 Finalising",
		Body:   "what changed",
		Branch: "norn/NORN-54/runner",
	})
	if err != nil {
		t.Fatalf("open a pull request: %v", err)
	}

	if address != "https://github.com/usenorn/runner/pull/231" {
		t.Fatalf("the pull request came back as %q", address)
	}
}

func TestABranchThatAlreadyHasAPullRequestIsAnsweredWithIt(t *testing.T) {
	forges := fake(t, "gh", `
if [ "$1" = "auth" ]; then exit 0; fi
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
  echo "https://github.com/usenorn/runner/pull/231"
  exit 0
fi
exit 1
`)

	address, err := forges.Existing(
		context.Background(), t.TempDir(), "norn/NORN-54/runner",
	)
	if err != nil {
		t.Fatalf("look for a pull request: %v", err)
	}

	if address != "https://github.com/usenorn/runner/pull/231" {
		t.Fatalf(
			"an open pull request was not found (%q), so asking for changes would open a second "+
				"one and split the review",
			address,
		)
	}
}

func TestABranchWithNoPullRequestIsNotAnErrorWorthStoppingFor(t *testing.T) {
	forges := fake(t, "gh", `
if [ "$1" = "auth" ]; then exit 0; fi
echo "no pull requests found for branch" >&2
exit 1
`)

	address, err := forges.Existing(
		context.Background(), t.TempDir(), "norn/NORN-54/runner",
	)
	if err != nil {
		t.Fatalf(
			"a branch with no pull request answered %v; there being none is the ordinary case "+
				"on a first run",
			err,
		)
	}

	if address != "" {
		t.Fatalf("a pull request was invented: %q", address)
	}
}

func TestAPullRequestSomebodyElseAlreadyOpenedIsUsedRatherThanReportedAsAFailure(t *testing.T) {
	forges := fake(t, "gh", `
if [ "$1" = "auth" ]; then exit 0; fi
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then exit 1; fi
echo "a pull request for branch norn/NORN-54/runner already exists: https://github.com/usenorn/runner/pull/9" >&2
exit 1
`)

	address, err := forges.Open(context.Background(), t.TempDir(), entity.PullRequest{
		Title:  "NORN-54 Finalising",
		Branch: "norn/NORN-54/runner",
	})
	if err != nil {
		t.Fatalf("open a pull request: %v", err)
	}

	if address != "https://github.com/usenorn/runner/pull/9" {
		t.Fatalf(
			"the pull request that already exists came back as %q; a run that cannot find it "+
				"reports no pull request at all for work that plainly has one",
			address,
		)
	}
}

func TestNoToolOnThePathIsSaidPlainlyRatherThanGuessedAt(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	forges := forgerepo.New(results())

	if _, available := forges.Available(context.Background(), t.TempDir()); available {
		t.Fatal("a machine with no gh and no glab claimed it could open pull requests")
	}

	_, err := forges.Open(context.Background(), t.TempDir(), entity.PullRequest{
		Branch: "norn/NORN-54/runner",
	})

	if err == nil || !strings.Contains(err.Error(), "signed in") {
		t.Fatalf("opening a pull request with no tool answered %v", err)
	}
}
