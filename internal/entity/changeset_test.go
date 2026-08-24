package entity_test

import (
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestAChangeSetGoesOutWithTheKeysNornActuallyReads(t *testing.T) {
	wire := entity.ChangeSet{Repositories: []entity.RepositoryChange{{
		Repository:   "backend",
		Branch:       "norn/NORN-54/backend",
		BaseSHA:      "base",
		HeadSHA:      "head",
		Commits:      3,
		Diffstat:     entity.Diffstat{Additions: 412, Deletions: 77, Files: 9},
		DiffArtifact: "f8b0a1c2-0000-4000-8000-000000000001",
		PullRequest:  "https://github.com/usenorn/runner/pull/231",
	}}}.Wire()

	if len(wire.Repos) != 1 {
		t.Fatalf("one repository went out as %d", len(wire.Repos))
	}

	held := wire.Repos[0]

	if held.Repository != "backend" || held.Branch != "norn/NORN-54/backend" {
		t.Fatalf("the repository went out as %+v", held)
	}

	if held.Files != 9 || held.Additions != 412 || held.Deletions != 77 || held.Commits != 3 {
		t.Fatalf(
			"the diffstat went out as %+v; these map onto differently named json keys, so a "+
				"mistake here shows up as zeroes on the review screen rather than an error",
			held,
		)
	}

	if held.Diff != "f8b0a1c2-0000-4000-8000-000000000001" {
		t.Fatalf("the diff artifact went out as %q", held.Diff)
	}
}

func TestMoreRepositoriesThanNornRecordsAreCutBackRatherThanRefused(t *testing.T) {
	many := entity.ChangeSet{}

	for index := range entity.ChangeSetMaxRepositories + 12 {
		many.Repositories = append(many.Repositories, entity.RepositoryChange{
			Repository: string(rune('a'+index%26)) + "-repository",
		})
	}

	wire := many.Wire()

	if len(wire.Repos) != entity.ChangeSetMaxRepositories {
		t.Fatalf(
			"%d repositories went out and norn refuses more than %d; a refused message closes "+
				"the connection rather than being answered, so everything else the run reported "+
				"would go with it",
			len(wire.Repos), entity.ChangeSetMaxRepositories,
		)
	}

	if many.Beyond() != 12 {
		t.Fatalf("%d repositories were left off and nothing says so", many.Beyond())
	}
}

func TestAValueLongerThanNornStoresIsCutRatherThanSentWhole(t *testing.T) {
	long := entity.ChangeSet{Repositories: []entity.RepositoryChange{{
		Repository: "backend",
		Branch:     strings.Repeat("b", entity.ChangeSetBranchMax+40),
		BaseSHA:    strings.Repeat("f", entity.ChangeSetRevisionMax+20),
	}}}.Wire()

	if len([]rune(long.Repos[0].Branch)) != entity.ChangeSetBranchMax {
		t.Fatalf("the branch went out at %d runes", len([]rune(long.Repos[0].Branch)))
	}

	if len([]rune(long.Repos[0].BaseSHA)) != entity.ChangeSetRevisionMax {
		t.Fatalf("the revision went out at %d runes", len([]rune(long.Repos[0].BaseSHA)))
	}
}

func TestACountThatCameBackNegativeNeverGoesOutThatWay(t *testing.T) {
	wire := entity.ChangeSet{Repositories: []entity.RepositoryChange{{
		Repository: "backend",
		Commits:    -1,
		Diffstat:   entity.Diffstat{Additions: -4},
	}}}.Wire()

	if wire.Repos[0].Commits != 0 || wire.Repos[0].Additions != 0 {
		t.Fatalf(
			"a negative count went out as %+v; norn refuses it, and a refused message closes the "+
				"connection",
			wire.Repos[0],
		)
	}
}

func TestTheAgentIsToldWhichFilesItLeftBehindAndWhereTheyAre(t *testing.T) {
	said := entity.CommitInjection([]entity.UncommittedWork{
		{Repository: "backend", Files: []string{"src/median.go", ".env.local"}},
	})

	for _, wanted := range []string{"backend", "src/median.go", ".env.local"} {
		if !strings.Contains(said, wanted) {
			t.Fatalf("the agent is never told about %q:\n%s", wanted, said)
		}
	}

	if !strings.Contains(said, "commit") {
		t.Fatalf("the agent is not actually asked to commit:\n%s", said)
	}
}

func TestAVeryLongListOfLeftoverFilesIsSummarisedRatherThanSentWhole(t *testing.T) {
	files := make([]string, 0, 200)

	for index := range 200 {
		files = append(files, "src/file"+string(rune('a'+index%26))+".go")
	}

	said := entity.CommitInjection([]entity.UncommittedWork{
		{Repository: "backend", Files: files},
	})

	if !strings.Contains(said, "and 180 more") {
		t.Fatalf(
			"200 leftover files went into the prompt whole; the instruction is injected into a "+
				"coding agent's turn, and a wall of paths buries what it is being asked to do:\n%s",
			said,
		)
	}
}

func TestOneCommitReadsAsOneCommitRatherThanOneCommits(t *testing.T) {
	one := entity.RepositoryChange{
		Commits:  1,
		Diffstat: entity.Diffstat{Additions: 1, Files: 1},
	}.Rendered()

	if !strings.Contains(one, "1 commit,") || !strings.Contains(one, "1 file") {
		t.Fatalf("a single commit reads as %q", one)
	}
}
