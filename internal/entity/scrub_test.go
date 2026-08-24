package entity_test

import (
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestWhatMustNotReachAForgeIsTakenOutOfThePullRequest(t *testing.T) {
	cases := []struct {
		name string
		text string
		gone string
		kind string
	}{
		{
			name: "an email address in the summary",
			text: "Fixed the retry bug. Ask rae@northwind.co if the numbers look wrong.",
			gone: "rae@northwind.co",
			kind: entity.ScrubbedAddress,
		},
		{
			name: "a co-author trailer on its own line",
			text: "Added the median helper.\n\nCo-Authored-By: Rae Chen <rae@northwind.co>\n",
			gone: "Co-Authored-By",
			kind: entity.ScrubbedTrailer,
		},
		{
			name: "an assistant attribution",
			text: "Added the median helper.\n\n🤖 Generated with Claude Code\n",
			gone: "Generated with",
			kind: entity.ScrubbedAttribute,
		},
		{
			name: "an agent token pasted into the notes",
			text: "Ran it with nrn_BN2JDWQWBnXGhUzTrPI7uQesdLe4pCpgTxDg89gCePk to check.",
			gone: "nrn_BN2JDWQWBnXGhUzTrPI7uQesdLe4pCpgTxDg89gCePk",
			kind: entity.ScrubbedSecret,
		},
		{
			name: "a forge token pasted into the notes",
			text: "Pushed with ghp_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8 by hand.",
			gone: "ghp_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8",
			kind: entity.ScrubbedSecret,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			scrubbed, kinds := entity.ScrubbedForForge(testCase.text)

			if strings.Contains(scrubbed, testCase.gone) {
				t.Fatalf(
					"%q survived into what norn would send a forge:\n%s",
					testCase.gone, scrubbed,
				)
			}

			if len(kinds) == 0 {
				t.Fatal("nothing was reported as removed, so the run would never say what went")
			}

			if kinds[0] != testCase.kind {
				t.Fatalf("reported %q removed, want %q", kinds[0], testCase.kind)
			}
		})
	}
}

func TestOrdinaryProseIsLeftExactlyAsTheAgentWroteIt(t *testing.T) {
	said := "Reproduced the retry bug in api, fixed the backoff, and proved it with a failing " +
		"test first.\n\nThe change is in internal/retry, and the test is beside it."

	scrubbed, kinds := entity.ScrubbedForForge(said)

	if scrubbed != said {
		t.Fatalf("prose with nothing wrong in it was rewritten:\n%s", scrubbed)
	}

	if kinds != nil {
		t.Fatalf("reported %v removed from prose that carried none of it", kinds)
	}
}

func TestWhatIsLeftAfterAScrubIsStillWorthReading(t *testing.T) {
	scrubbed, _ := entity.ScrubbedForForge(
		"Added the median helper and covered it.\n\nCo-Authored-By: Rae <rae@northwind.co>\n",
	)

	if !strings.Contains(scrubbed, "Added the median helper and covered it.") {
		t.Fatalf(
			"the summary went with the trailer:\n%s\nA reviewer loses what the run did over one "+
				"bad line",
			scrubbed,
		)
	}
}
