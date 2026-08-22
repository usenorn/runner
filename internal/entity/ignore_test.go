package entity_test

import (
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func decide(t *testing.T, nornignore, relPath string, isDir bool) entity.IgnoreDecision {
	t.Helper()

	return entity.NewIgnoreSet(entity.ParseIgnore(nornignore)).Decide(relPath, isDir)
}

func TestTheDenylistRefusesSecretsWhateverElseIsWritten(t *testing.T) {
	cases := []struct {
		name    string
		relPath string
		isDir   bool
	}{
		{"a bare env file", ".env", false},
		{"an env file per environment", "backend/.env.production", false},
		{"an env file deep in the folder", "a/b/c/.env", false},
		{"a private key", "certs/server.key", false},
		{"a certificate", "certs/server.pem", false},
		{"an ssh key", "keys/id_rsa", false},
		{"an ssh key with a suffix", "keys/id_ed25519.pub", false},
		{"a netrc", ".netrc", false},
		{"anything norn keeps in the folder", ".norn/codebase.yaml", false},
		{"an ssh directory", ".ssh", true},
		{"anything inside an ssh directory", ".ssh/known_hosts", false},
		{"aws credentials", ".aws/credentials", false},
		{"gcloud credentials at any depth", "home/vlad/.config/gcloud/adc.json", false},
		{"a kube config at any depth", "infra/.kube/config", false},
		{"a docker config at any depth", "infra/.docker/config.json", false},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := decide(t, "", test.relPath, test.isDir); got != entity.IgnoreDenied {
				t.Fatalf(
					"%s was not denied; the denylist is the one rule a snapshot may not break, "+
						"because these files must never reach a coding agent",
					test.relPath,
				)
			}
		})
	}
}

func TestNoNornignoreRuleCanReIncludeADeniedFile(t *testing.T) {
	attempts := []string{
		"!.env",
		"!/.env",
		"!**/.env",
		"!*",
		"!.env\n!.norn/\n!*.pem",
	}

	for _, attempt := range attempts {
		t.Run(attempt, func(t *testing.T) {
			for _, relPath := range []string{".env", ".norn/codebase.yaml", "certs/a.pem"} {
				if got := decide(t, attempt, relPath, false); got != entity.IgnoreDenied {
					t.Fatalf(
						"%q re-included %s; the built-in denylist is not something a folder may "+
							"argue with",
						attempt, relPath,
					)
				}
			}
		})
	}
}

func TestTheBuiltInIgnoresSkipTheThingsThatAreNotSource(t *testing.T) {
	cases := []struct {
		relPath string
		isDir   bool
	}{
		{"node_modules", true},
		{"web/node_modules/left-pad/index.js", false},
		{"target", true},
		{"backend/dist/app.js", false},
		{".venv/bin/python", false},
		{"__pycache__/mod.pyc", false},
		{".idea/workspace.xml", false},
		{"logs/runner.log", false},
		{".DS_Store", false},
	}

	for _, test := range cases {
		t.Run(test.relPath, func(t *testing.T) {
			if got := decide(t, "", test.relPath, test.isDir); got != entity.IgnoreSkip {
				t.Fatalf(
					"%s was kept; copying it into an execution costs time and disk and teaches the "+
						"coding agent nothing",
					test.relPath,
				)
			}
		})
	}
}

func TestANornignoreCanReIncludeSomethingTheDefaultsSkipped(t *testing.T) {
	if got := decide(t, "!node_modules/", "node_modules", true); got != entity.IgnoreKeep {
		t.Fatalf(
			"node_modules stayed skipped; the defaults are a starting point a folder is allowed to " +
				"override, unlike the denylist",
		)
	}

	if got := decide(t, "!*.log", "logs/runner.log", false); got != entity.IgnoreKeep {
		t.Fatalf("a nornignore could not re-include a log file the defaults skip")
	}
}

func TestAFileUnderASkippedDirectoryStaysSkippedHoweverItIsNamed(t *testing.T) {
	set := "vendor/\n!vendor/keep.txt"

	if got := decide(t, set, "vendor/keep.txt", false); got != entity.IgnoreSkip {
		t.Fatalf(
			"vendor/keep.txt was kept; git never descends into an ignored directory, so a rule " +
				"under one cannot bring a file back and the snapshot must behave the same way",
		)
	}
}

func TestANornignoreSkipsWhatItSays(t *testing.T) {
	cases := []struct {
		name    string
		rules   string
		relPath string
		isDir   bool
		want    entity.IgnoreDecision
	}{
		{"a plain name at any depth", "scratch\n", "a/b/scratch", true, entity.IgnoreSkip},
		{"a rooted name only at the root", "/scratch\n", "a/scratch", true, entity.IgnoreKeep},
		{"a rooted name at the root", "/scratch\n", "scratch", true, entity.IgnoreSkip},
		{"a directory rule against a file", "scratch/\n", "scratch", false, entity.IgnoreKeep},
		{"a glob", "*.tmp\n", "a/b/c.tmp", false, entity.IgnoreSkip},
		{"a double star", "docs/**/draft\n", "docs/a/b/draft", true, entity.IgnoreSkip},
		{"a character class", "note[0-9].txt\n", "note4.txt", false, entity.IgnoreSkip},
		{"a comment", "# scratch\n", "scratch", true, entity.IgnoreKeep},
		{"a blank line", "\n\n", "scratch", true, entity.IgnoreKeep},
		{"the last rule wins", "a.txt\n!a.txt\n", "a.txt", false, entity.IgnoreKeep},
		{"the last rule wins the other way", "!a.txt\na.txt\n", "a.txt", false, entity.IgnoreSkip},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := decide(t, test.rules, test.relPath, test.isDir); got != test.want {
				t.Fatalf(
					"%q against %s decided %d and should have decided %d",
					test.rules, test.relPath, got, test.want,
				)
			}
		})
	}
}

func TestTheRootOfTheFolderIsNeverExcluded(t *testing.T) {
	if got := decide(t, "*\n", ".", true); got != entity.IgnoreKeep {
		t.Fatalf("the folder itself was excluded, which would make every snapshot empty")
	}
}
