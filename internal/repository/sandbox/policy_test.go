package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func policyForTest() hostPolicy {
	return hostPolicy{
		home:      "/Users/rae",
		state:     "/Users/rae/.norn",
		socket:    "/Users/rae/.norn/runner.sock",
		readable:  []string{"/Users/rae/.local/share/claude", "/Users/rae/.norn/runs/exec-1/metadata"},
		writable:  []string{"/Users/rae/.norn/runs/exec-1/workspace", "/Users/rae/code/api/.git"},
		protected: []string{"/Users/rae/code/api/.git/config", "/Users/rae/code/api/.git/hooks"},
	}
}

func TestSeatbeltDeniesTheProtectedGitFilesAfterItAllowsTheCommonDir(t *testing.T) {
	profile := seatbelt(policyForTest())

	allowed := strings.Index(profile, `(allow file-write* (subpath "/dev")`)
	protected := strings.Index(profile, `(deny file-write* (subpath "/Users/rae/code/api/.git/config")`)

	if allowed < 0 || protected < allowed {
		t.Fatalf(
			"the profile does not deny the git config after allowing the common dir, so the last "+
				"rule to match lets the agent plant a hook:\n%s",
			profile,
		)
	}
}

func TestSeatbeltHidesTheHomeFolderButTheRunCanStillReadWhatItNeeds(t *testing.T) {
	profile := seatbelt(policyForTest())

	hidden := strings.Index(profile, `(deny file-read-data file-read-xattr (subpath "/Users/rae")`)
	shown := strings.Index(profile, `(allow file-read-data file-read-xattr (subpath "/Users/rae/.local/share/claude")`)

	if hidden < 0 || shown < hidden {
		t.Fatalf("the home folder is not hidden before the allowlist is shown:\n%s", profile)
	}

	for _, refused := range []string{
		`(deny network-outbound (remote unix-socket))`,
		`(allow network-outbound (remote unix-socket (path-literal "/Users/rae/.norn/runner.sock")))`,
		`(global-name "com.apple.SecurityServer")`,
		`(deny appleevent-send)`,
	} {
		if !strings.Contains(profile, refused) {
			t.Errorf("the profile lacks %s:\n%s", refused, profile)
		}
	}
}

func TestSeatbeltQuotesAPathSoItCannotEndTheRule(t *testing.T) {
	policy := policyForTest()
	policy.readable = []string{`/Users/rae/odd") (allow file-write* (subpath "/`}

	if strings.Contains(seatbelt(policy), `(allow file-write* (subpath "/")`) {
		t.Fatal("a folder name wrote its own rule into the profile")
	}
}

func TestBubblewrapHidesTheHomeFolderAndBindsTheProtectedFilesLast(t *testing.T) {
	args := bubblewrap(policyForTest(), "/usr/bin/bwrap", []string{"claude", "--print"})

	home := slices.Index(args, "/Users/rae")
	writable := slices.Index(args, "/Users/rae/code/api/.git")
	protected := slices.Index(args, "/Users/rae/code/api/.git/config")

	if home < 0 || args[home-1] != "--tmpfs" {
		t.Fatalf("the home folder is not replaced with an empty one: %v", args)
	}

	if writable < home || protected < writable || args[protected-1] != "--ro-bind-try" {
		t.Fatalf("the git config is not bound read-only after the common dir: %v", args)
	}

	if tail := args[len(args)-3:]; !slices.Equal(tail, []string{"--", "claude", "--print"}) {
		t.Fatalf("the command does not follow the sandbox's own arguments: %v", tail)
	}
}

func TestNothingOnThePathOpensTheWholeHomeFolder(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")

	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, "tool.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(filepath.Join(home, "tool.sh"), filepath.Join(bin, "tool")); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", strings.Join([]string{home, bin, "/usr/bin"}, string(filepath.ListSeparator)))

	readable := installed(home)

	if slices.Contains(readable, canonical(home)) {
		t.Fatalf(
			"the home folder itself became readable through the path, which hands the agent every "+
				"credential in it: %v",
			readable,
		)
	}

	if !slices.Contains(readable, canonical(bin)) {
		t.Fatalf("a tool folder on the path under home is no longer readable: %v", readable)
	}
}

func TestAMissingHooksFolderIsCreatedSoTheAgentCannotPlantOne(t *testing.T) {
	common := filepath.Join(t.TempDir(), ".git")
	hooks := filepath.Join(common, "hooks")
	unmade := filepath.Join(t.TempDir(), "workspace", "api", ".git")

	if err := os.MkdirAll(common, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := materialise([]string{hooks, unmade}); err != nil {
		t.Fatalf("materialise: %v", err)
	}

	if info, err := os.Stat(hooks); err != nil || !info.IsDir() {
		t.Fatalf(
			"the missing hooks folder was not created, so the agent could create one the person's "+
				"own git would then run: %v",
			err,
		)
	}

	if _, err := os.Lstat(unmade); err == nil {
		t.Fatal("a git file the checkout has yet to write was created ahead of it")
	}
}
