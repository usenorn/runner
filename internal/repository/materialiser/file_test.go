package materialiser_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
	materialiserrepo "github.com/usenorn/runner/internal/repository/materialiser"
)

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(body)
}

func source(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	write(t, filepath.Join(root, "AGENTS.md"), "rules\n", 0o644)
	write(t, filepath.Join(root, "scripts", "build.sh"), "#!/bin/sh\n", 0o755)
	write(t, filepath.Join(root, "node_modules", "left-pad", "index.js"), "1\n", 0o644)
	write(t, filepath.Join(root, "docs", "guide.md"), "read me\n", 0o644)

	return root
}

func TestEverythingTheSkipRuleKeepsArrivesWithItsContentAndItsMode(t *testing.T) {
	root, into := source(t), filepath.Join(t.TempDir(), "workspace")

	result, err := materialiserrepo.New().Copy(context.Background(), root, into, nil, 0)
	if err != nil {
		t.Fatalf("copy %s: %v", root, err)
	}

	if body := read(t, filepath.Join(into, "AGENTS.md")); body != "rules\n" {
		t.Fatalf("AGENTS.md arrived as %q", body)
	}

	info, err := os.Stat(filepath.Join(into, "scripts", "build.sh"))
	if err != nil {
		t.Fatalf("stat build.sh: %v", err)
	}

	if info.Mode().Perm() != 0o755 {
		t.Fatalf(
			"build.sh arrived as %v; a script that loses its executable bit cannot be run by the "+
				"coding agent that was given it",
			info.Mode().Perm(),
		)
	}

	if len(result.Files) != 4 {
		t.Fatalf("%d files were recorded and four were copied", len(result.Files))
	}

	for _, file := range result.Files {
		if file.Method != entity.MaterialiseReflink && file.Method != entity.MaterialiseCopy {
			t.Fatalf("%s says it arrived by %q", file.RelPath, file.Method)
		}
	}
}

func TestASkippedDirectoryIsNeverWalkedIntoAtAll(t *testing.T) {
	root, into := source(t), filepath.Join(t.TempDir(), "workspace")

	skip := func(relPath string, _ bool) bool { return relPath == "node_modules" }

	result, err := materialiserrepo.New().Copy(context.Background(), root, into, skip, 0)
	if err != nil {
		t.Fatalf("copy %s: %v", root, err)
	}

	if _, err := os.Stat(filepath.Join(into, "node_modules")); err == nil {
		t.Fatalf(
			"node_modules was copied; the point of skipping a folder is that nothing under it is " +
				"read, not that it is copied and then deleted",
		)
	}

	for _, file := range result.Files {
		if strings.HasPrefix(file.RelPath, "node_modules") {
			t.Fatalf("%s was recorded even though its folder was skipped", file.RelPath)
		}
	}
}

func TestAFolderWhoseContentsWereAllSkippedDoesNotArriveEmpty(t *testing.T) {
	root, into := source(t), filepath.Join(t.TempDir(), "workspace")

	write(t, filepath.Join(root, "certs", "server.pem"), "-----BEGIN KEY-----\n", 0o600)

	skip := func(relPath string, _ bool) bool { return strings.HasSuffix(relPath, ".pem") }

	if _, err := materialiserrepo.New().Copy(context.Background(), root, into, skip, 0); err != nil {
		t.Fatalf("copy %s: %v", root, err)
	}

	if _, err := os.Stat(filepath.Join(into, "certs")); err == nil {
		t.Fatalf(
			"an empty certs folder arrived in the snapshot; a folder that holds nothing but " +
				"excluded files reads as though something is in it",
		)
	}

	if _, err := os.Stat(filepath.Join(into, "docs", "guide.md")); err != nil {
		t.Fatalf("a folder that does hold something was pruned as well: %v", err)
	}
}

func TestALinkPointingOutOfTheFolderIsNotFollowed(t *testing.T) {
	root, into := source(t), filepath.Join(t.TempDir(), "workspace")
	outside := filepath.Join(t.TempDir(), "secrets.txt")

	write(t, outside, "shhh\n", 0o600)

	if err := os.Symlink(outside, filepath.Join(root, "secrets.txt")); err != nil {
		t.Fatalf("link to %s: %v", outside, err)
	}

	if err := os.Symlink("docs/guide.md", filepath.Join(root, "guide.md")); err != nil {
		t.Fatalf("link to docs/guide.md: %v", err)
	}

	result, err := materialiserrepo.New().Copy(context.Background(), root, into, nil, 0)
	if err != nil {
		t.Fatalf("copy %s: %v", root, err)
	}

	if _, err := os.Lstat(filepath.Join(into, "secrets.txt")); err == nil {
		t.Fatalf(
			"a link reaching outside the folder was carried across; following it would put a file " +
				"the person never connected inside an execution",
		)
	}

	if len(result.Warnings) == 0 {
		t.Fatalf("the outward link was dropped without saying so")
	}

	target, err := os.Readlink(filepath.Join(into, "guide.md"))
	if err != nil || target != "docs/guide.md" {
		t.Fatalf("a link inside the folder became %q, %v", target, err)
	}
}

func TestAFolderBiggerThanTheBudgetIsRefusedRatherThanHalfCopied(t *testing.T) {
	root, into := source(t), filepath.Join(t.TempDir(), "workspace")

	write(t, filepath.Join(root, "big.bin"), strings.Repeat("x", 4096), 0o644)

	_, err := materialiserrepo.New().Copy(context.Background(), root, into, nil, 1024)
	if !errors.Is(err, entity.ErrSnapshotTooLarge) {
		t.Fatalf(
			"copying past the budget returned %v; a snapshot that quietly fills the disk is worse "+
				"than one that refuses and says which file did it",
			err,
		)
	}

	if !strings.Contains(err.Error(), "big.bin") {
		t.Fatalf("the refusal does not name the file that crossed the budget: %v", err)
	}
}

func TestNamedPathsAreCopiedAndAFolderAmongThemIsReported(t *testing.T) {
	root, into := source(t), filepath.Join(t.TempDir(), "workspace")

	result, err := materialiserrepo.New().CopyPaths(
		context.Background(), root, into, []string{"AGENTS.md", "docs", "missing.txt"},
	)
	if err != nil {
		t.Fatalf("copy the named paths: %v", err)
	}

	if body := read(t, filepath.Join(into, "AGENTS.md")); body != "rules\n" {
		t.Fatalf("AGENTS.md arrived as %q", body)
	}

	if len(result.Files) != 1 {
		t.Fatalf("%d files were copied and one was a file", len(result.Files))
	}

	if len(result.Warnings) != 2 {
		t.Fatalf("%d notes were left for a folder and a path that is not there", len(result.Warnings))
	}
}

func TestRemovingASnapshotTakesTheWholeTreeWithIt(t *testing.T) {
	root, into := source(t), filepath.Join(t.TempDir(), "workspace")
	materialiser := materialiserrepo.New()

	if _, err := materialiser.Copy(context.Background(), root, into, nil, 0); err != nil {
		t.Fatalf("copy %s: %v", root, err)
	}

	if err := materialiser.Remove(context.Background(), into); err != nil {
		t.Fatalf("remove %s: %v", into, err)
	}

	if _, err := os.Stat(into); err == nil {
		t.Fatalf("%s is still there after it was removed", into)
	}
}
