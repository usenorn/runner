package entity_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func repo(dir, gitDir, commonDir, topLevel string) entity.GitFacts {
	return entity.GitFacts{
		Dir:            dir,
		GitDir:         gitDir,
		CommonDir:      commonDir,
		TopLevel:       topLevel,
		InsideWorkTree: true,
	}
}

func folder() []entity.GitFacts {
	return []entity.GitFacts{
		repo("/w/norn", "/w/norn/.git", "/w/norn/.git", "/w/norn"),
		repo("/w/runner", "/w/runner/.git", "/w/runner/.git", "/w/runner"),
		repo("/w/wt", "/w/norn/.git/worktrees/wt", "/w/norn/.git", "/w/wt"),
		repo("/w/norn/scratch", "/w/norn/scratch/.git", "/w/norn/scratch/.git", "/w/norn/scratch"),
		func() entity.GitFacts {
			facts := repo("/w/norn/web/ui", "/w/norn/.git/modules/web/ui",
				"/w/norn/.git/modules/web/ui", "/w/norn/web/ui")
			facts.Superproject = "/w/norn"

			return facts
		}(),
		{Dir: "/w/archive/old.git", GitDir: "/w/archive/old.git", CommonDir: "/w/archive/old.git", Bare: true},
	}
}

func classified(t *testing.T, relPath string) entity.Repository {
	t.Helper()

	for _, repository := range entity.Classify("/w", folder()) {
		if repository.RelPath == relPath {
			return repository
		}
	}

	t.Fatalf("%s was not classified at all; the scan would never mention it", relPath)

	return entity.Repository{}
}

func TestAPlainRepositoryIsListedUnderItsOwnName(t *testing.T) {
	norn := classified(t, "norn")

	if norn.Kind != entity.RepositoryNormal {
		t.Fatalf("norn came back as %q, want %q", norn.Kind, entity.RepositoryNormal)
	}

	if norn.Name != "norn" {
		t.Fatalf("norn is named %q, want the folder's own name", norn.Name)
	}
}

func TestALinkedWorktreeIsListedAndKeepsTheGitDirItShares(t *testing.T) {
	worktree := classified(t, "wt")

	if worktree.Kind != entity.RepositoryWorktree {
		t.Fatalf("a linked worktree came back as %q, want %q", worktree.Kind, entity.RepositoryWorktree)
	}

	if worktree.CommonDir != "/w/norn/.git" {
		t.Fatalf(
			"the worktree recorded common git dir %q; the snapshot engine cannot share an object "+
				"store it was never told about",
			worktree.CommonDir,
		)
	}
}

func TestASubmoduleBelongsToItsParentAndIsNotOfferedOnItsOwn(t *testing.T) {
	submodule := classified(t, "norn/web/ui")

	if submodule.Kind != entity.RepositorySubmodule {
		t.Fatalf("a submodule came back as %q, want %q", submodule.Kind, entity.RepositorySubmodule)
	}

	if submodule.Parent != "norn" {
		t.Fatalf("the submodule names %q as its parent, want norn", submodule.Parent)
	}

	if submodule.Kind.Listed() {
		t.Fatalf("a submodule was listed as a repository in its own right")
	}
}

func TestARepositoryInsideAnotherIsListedOnItsOwnAndNamesTheOneItSitsIn(t *testing.T) {
	nested := classified(t, "norn/scratch")

	if nested.Kind != entity.RepositoryNested {
		t.Fatalf("a nested repository came back as %q, want %q", nested.Kind, entity.RepositoryNested)
	}

	if !nested.Kind.Listed() {
		t.Fatalf("a nested repository was not listed; the code in it would never be offered")
	}

	if nested.Parent != "norn" {
		t.Fatalf(
			"the nested repository names %q as the repository it sits in, want norn; the parent's "+
				"snapshot has to exclude that subtree",
			nested.Parent,
		)
	}
}

func TestABareRepositoryIsRecordedButNeverOffered(t *testing.T) {
	bare := classified(t, "archive/old.git")

	if bare.Kind != entity.RepositoryBare {
		t.Fatalf("a bare repository came back as %q, want %q", bare.Kind, entity.RepositoryBare)
	}

	if bare.Kind.Listed() {
		t.Fatalf("a bare repository was listed; there is no working tree to run an agent in")
	}
}

func TestADirectoryThatIsNotItsOwnRepositoryRootIsNotARepository(t *testing.T) {
	facts := repo("/w/norn/docs", "/w/norn/.git", "/w/norn/.git", "/w/norn")

	for _, repository := range entity.Classify("/w", []entity.GitFacts{facts}) {
		t.Fatalf(
			"a subdirectory of a repository was classified as %q at %q; only a repository root is "+
				"a repository",
			repository.Kind, repository.RelPath,
		)
	}
}

func TestANestedRepositoryNamesTheNearestRepositoryAroundIt(t *testing.T) {
	facts := []entity.GitFacts{
		repo("/w/outer", "/w/outer/.git", "/w/outer/.git", "/w/outer"),
		repo("/w/outer/middle", "/w/outer/middle/.git", "/w/outer/middle/.git", "/w/outer/middle"),
		repo("/w/outer/middle/inner", "/w/outer/middle/inner/.git",
			"/w/outer/middle/inner/.git", "/w/outer/middle/inner"),
	}

	for _, repository := range entity.Classify("/w", facts) {
		if repository.RelPath != "outer/middle/inner" {
			continue
		}

		if repository.Parent != "outer/middle" {
			t.Fatalf(
				"the innermost repository names %q as its parent, want outer/middle; excluding it "+
					"from the wrong subtree leaves it in two snapshots",
				repository.Parent,
			)
		}

		return
	}

	t.Fatalf("the innermost repository was not classified")
}

func TestEveryRepositoryKindIsDecidedDeliberately(t *testing.T) {
	for _, kind := range entity.RepositoryKinds() {
		if !kind.Valid() {
			t.Fatalf("%q is in the kind list but does not validate", kind)
		}
	}

	if entity.RepositoryKind("checkout").Valid() {
		t.Fatalf("an invented repository kind validated")
	}
}

func TestTheSameRemoteFingerprintsTheSameHoweverItIsSpelled(t *testing.T) {
	spellings := []string{
		"https://github.com/usenorn/norn.git",
		"https://GitHub.com/usenorn/norn",
		"git@github.com:usenorn/norn.git",
		"ssh://git@github.com/usenorn/norn.git",
		"https://someone:secret@github.com/usenorn/norn.git",
	}

	first := entity.FingerprintRemote(spellings[0])

	if first.Hash == "" {
		t.Fatalf("a github remote produced no hash at all")
	}

	for _, spelling := range spellings[1:] {
		if got := entity.FingerprintRemote(spelling); got.Hash != first.Hash {
			t.Fatalf(
				"%q hashed to %q and %q hashed to %q; the same remote on two machines has to match",
				spellings[0], first.Hash, spelling, got.Hash,
			)
		}
	}

	if first.Host != "github.com" || first.PathTail != "usenorn/norn" {
		t.Fatalf(
			"the fingerprint reads %q %q, want github.com and usenorn/norn for a person to "+
				"recognise it by",
			first.Host, first.PathTail,
		)
	}
}

func TestAFingerprintNeverCarriesTheCredentialsInTheUrl(t *testing.T) {
	fingerprint := entity.FingerprintRemote("https://someone:secret@github.com/usenorn/norn.git")

	for _, field := range []string{fingerprint.Hash, fingerprint.Host, fingerprint.PathTail} {
		if field == "" {
			continue
		}

		for _, secret := range []string{"someone", "secret"} {
			if strings.Contains(field, secret) {
				t.Fatalf("%q leaked %q out of the remote url", field, secret)
			}
		}
	}
}

func TestDifferentRemotesDoNotShareAFingerprint(t *testing.T) {
	norn := entity.FingerprintRemote("git@github.com:usenorn/norn.git")
	runner := entity.FingerprintRemote("git@github.com:usenorn/runner.git")

	if norn.Hash == runner.Hash {
		t.Fatalf("two different repositories share the hash %q", norn.Hash)
	}
}

func TestARepositoryWithNoRemoteHasNoFingerprint(t *testing.T) {
	if got := entity.FingerprintRemote("   "); got.Known() {
		t.Fatalf("a repository with no remote produced the fingerprint %+v", got)
	}
}

func inventory(repositories ...entity.Repository) entity.Inventory {
	return entity.Inventory{Name: "norn", RootPath: "/w", Repositories: repositories}
}

func listed(relPath, branch string) entity.Repository {
	return entity.Repository{
		Name:          relPath,
		RelPath:       relPath,
		Kind:          entity.RepositoryNormal,
		DefaultBranch: branch,
	}
}

func TestDriftIsAboutWhichRepositoriesAreThereRatherThanTheOrderTheyWereFoundIn(t *testing.T) {
	before := inventory(listed("norn", "main"), listed("runner", "main"))
	after := inventory(listed("runner", "main"), listed("norn", "main"))

	if drift := entity.DriftBetween(before, after); drift.Any() {
		t.Fatalf("finding the same repositories in a different order reported drift: %+v", drift)
	}
}

func TestAddingARepositoryIsDrift(t *testing.T) {
	drift := entity.DriftBetween(
		inventory(listed("norn", "main")),
		inventory(listed("norn", "main"), listed("runner", "main")),
	)

	if !slices.Equal(drift.Added, []string{"runner"}) {
		t.Fatalf("adding a repository reported added %v, want [runner]", drift.Added)
	}
}

func TestRemovingARepositoryIsDrift(t *testing.T) {
	drift := entity.DriftBetween(
		inventory(listed("norn", "main"), listed("runner", "main")),
		inventory(listed("norn", "main")),
	)

	if !slices.Equal(drift.Removed, []string{"runner"}) {
		t.Fatalf("removing a repository reported removed %v, want [runner]", drift.Removed)
	}
}

func TestARepositoryThatChangedItsDefaultBranchIsDrift(t *testing.T) {
	drift := entity.DriftBetween(
		inventory(listed("norn", "master")),
		inventory(listed("norn", "main")),
	)

	if !slices.Equal(drift.Changed, []string{"norn"}) {
		t.Fatalf(
			"renaming a default branch reported changed %v, want [norn]; norn stores the branch, "+
				"so the server calls this drift and the machine has to agree",
			drift.Changed,
		)
	}
}

func TestSomethingTheServerNeverSeesIsNotDrift(t *testing.T) {
	submodule := entity.Repository{
		Name:    "ui",
		RelPath: "norn/web/ui",
		Kind:    entity.RepositorySubmodule,
		Parent:  "norn",
	}

	drift := entity.DriftBetween(
		inventory(listed("norn", "main")),
		inventory(listed("norn", "main"), submodule),
	)

	if drift.Any() {
		t.Fatalf(
			"a submodule appearing reported drift %+v; it is never sent, so the server would "+
				"stay active while the machine claimed otherwise",
			drift,
		)
	}
}

func TestACodebaseIsDriftedWhenWhatWasReportedIsNotWhatWasConfirmed(t *testing.T) {
	codebase := entity.Codebase{
		Confirmed: inventory(listed("norn", "main")),
		Reported:  inventory(listed("norn", "main"), listed("runner", "main")),
	}

	if !codebase.Drifted() {
		t.Fatalf("a codebase reporting more than was confirmed does not read as drifted")
	}

	codebase.Confirmed = codebase.Reported

	if codebase.Drifted() {
		t.Fatalf("a codebase whose two inventories agree still reads as drifted")
	}
}
