package scanner_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	scannerrepo "github.com/usenorn/runner/internal/repository/scanner"
)

func settings() config.Codebase {
	return config.Codebase{
		ScanDepth:      entity.ScanDepthDefault,
		RescanInterval: 6 * time.Hour,
		ProbeTimeout:   30 * time.Second,
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=norn",
		"GIT_AUTHOR_EMAIL=norn@example.com",
		"GIT_COMMITTER_NAME=norn",
		"GIT_COMMITTER_EMAIL=norn@example.com",
	)

	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func repository(t *testing.T, path, remote string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}

	git(t, path, "init", "-b", "main", "-q")
	write(t, filepath.Join(path, "README.md"), "hello\n")
	git(t, path, "add", "README.md")
	git(t, path, "commit", "-q", "-m", "first")

	if remote != "" {
		git(t, path, "remote", "add", "origin", remote)
	}
}

func folder(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed, so a real folder cannot be built to scan")
	}

	base := t.TempDir()
	source := filepath.Join(base, "source", "ui")
	root := filepath.Join(base, "work")

	repository(t, source, "")
	repository(t, filepath.Join(root, "norn"), "https://github.com/usenorn/norn.git")
	repository(t, filepath.Join(root, "runner"), "git@github.com:usenorn/runner.git")
	repository(t, filepath.Join(root, "norn", "scratch"), "")

	git(t, filepath.Join(root, "norn"),
		"-c", "protocol.file.allow=always", "submodule", "--quiet", "add", source, "web/ui")
	git(t, filepath.Join(root, "norn"), "commit", "-q", "-m", "add ui")

	git(t, filepath.Join(root, "norn"), "worktree", "add", "--detach", "-q", "../wt", "HEAD")

	if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatalf("create archive: %v", err)
	}

	bare := filepath.Join(root, "archive", "old.git")

	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	write(t, filepath.Join(root, "AGENTS.md"), "rules\n")
	write(t, filepath.Join(root, "Makefile"), "all:\n")
	write(t, filepath.Join(root, "node_modules", "left-pad", "index.js"), "module.exports = 1\n")

	return root
}

func scan(t *testing.T, root string) ([]entity.Repository, []string) {
	t.Helper()

	scanned, err := scannerrepo.New(settings()).Scan(context.Background(), root, entity.ScanDepthDefault)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}

	return entity.Classify(scanned.Root, scanned.Repositories), scanned.SharedFiles
}

func found(t *testing.T, repositories []entity.Repository, relPath string) entity.Repository {
	t.Helper()

	for _, repository := range repositories {
		if repository.RelPath == relPath {
			return repository
		}
	}

	t.Fatalf("%s is missing from the scan; found %v", relPath, names(repositories))

	return entity.Repository{}
}

func names(repositories []entity.Repository) []string {
	found := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		found = append(found, repository.RelPath+":"+string(repository.Kind))
	}

	return found
}

func TestAFolderOfSeveralRepositoriesIsReadWithoutBeingRestructured(t *testing.T) {
	repositories, shared := scan(t, folder(t))

	kinds := map[string]entity.RepositoryKind{
		"norn":            entity.RepositoryNormal,
		"runner":          entity.RepositoryNormal,
		"wt":              entity.RepositoryWorktree,
		"norn/scratch":    entity.RepositoryNested,
		"norn/web/ui":     entity.RepositorySubmodule,
		"archive/old.git": entity.RepositoryBare,
	}

	for relPath, want := range kinds {
		if got := found(t, repositories, relPath).Kind; got != want {
			t.Fatalf("%s was classified %q, want %q", relPath, got, want)
		}
	}

	if len(repositories) != len(kinds) {
		t.Fatalf("the scan found %v, want exactly the six repositories in the folder", names(repositories))
	}

	for _, name := range []string{"AGENTS.md", "Makefile"} {
		if !contains(shared, name) {
			t.Fatalf("%s at the root was not recorded as a shared file; found %v", name, shared)
		}
	}
}

func TestARepositoryCarriesTheRemoteItPointsAtWithoutTheUrl(t *testing.T) {
	repositories, _ := scan(t, folder(t))

	norn := found(t, repositories, "norn").Remote

	if norn.Host != "github.com" || norn.PathTail != "usenorn/norn" {
		t.Fatalf("norn's remote reads %+v, want github.com and usenorn/norn", norn)
	}

	if norn.Hash == "" {
		t.Fatalf("norn's remote has no hash, so the same repository on another machine cannot be recognised")
	}
}

func TestALinkedWorktreeKeepsTheObjectStoreItShares(t *testing.T) {
	root := folder(t)
	repositories, _ := scan(t, root)

	worktree := found(t, repositories, "wt")

	want, err := filepath.EvalSymlinks(filepath.Join(root, "norn", ".git"))
	if err != nil {
		t.Fatalf("resolve norn's git dir: %v", err)
	}

	if worktree.CommonDir != want {
		t.Fatalf("the worktree points at %q, want %q", worktree.CommonDir, want)
	}
}

func TestTheDefaultBranchOfEachRepositoryIsRead(t *testing.T) {
	repositories, _ := scan(t, folder(t))

	if got := found(t, repositories, "runner").DefaultBranch; got != "main" {
		t.Fatalf("runner's default branch reads %q, want main", got)
	}
}

func TestScanningAFolderChangesNothingInIt(t *testing.T) {
	root := folder(t)

	before := fingerprint(t, root)

	scan(t, root)
	scan(t, root)

	after := fingerprint(t, root)

	for path, want := range before {
		got, present := after[path]

		switch {
		case !present:
			t.Fatalf("%s disappeared during the scan", path)
		case got != want:
			t.Fatalf(
				"%s changed during the scan (%s became %s); a scan reads a person's folder and "+
					"must never write to it",
				path, want, got,
			)
		}
	}

	for path := range after {
		if _, present := before[path]; !present {
			t.Fatalf("the scan created %s inside the folder", path)
		}
	}
}

func fingerprint(t *testing.T, root string) map[string]string {
	t.Helper()

	tree := map[string]string{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		stamp := fmt.Sprintf("%s|%d|%s", info.Mode(), info.Size(), info.ModTime().UTC())

		if entry.IsDir() {
			tree[relative] = "dir|" + info.Mode().String()

			return nil
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}

			tree[relative] = "link|" + target

			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		sum := sha256.Sum256(body)
		tree[relative] = stamp + "|" + hex.EncodeToString(sum[:])

		return nil
	})
	if err != nil {
		t.Fatalf("fingerprint %s: %v", root, err)
	}

	return tree
}

func contains(haystack []string, needle string) bool {
	for _, straw := range haystack {
		if straw == needle {
			return true
		}
	}

	return false
}
