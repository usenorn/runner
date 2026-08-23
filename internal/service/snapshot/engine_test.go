package snapshot_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
	materialiserrepo "github.com/usenorn/runner/internal/repository/materialiser"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	scannerrepo "github.com/usenorn/runner/internal/repository/scanner"
	settingsrepo "github.com/usenorn/runner/internal/repository/settings"
	worktreerepo "github.com/usenorn/runner/internal/repository/worktree"
	"github.com/usenorn/runner/internal/service"
	snapshotsvc "github.com/usenorn/runner/internal/service/snapshot"
)

func run(t *testing.T, dir string, args ...string) string {
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

	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}

	return strings.TrimSpace(string(out))
}

func started(t *testing.T, path, remote string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}

	run(t, path, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(path, "README.md"), "hello\n")
	run(t, path, "add", "README.md")
	run(t, path, "commit", "-q", "-m", "first")

	if remote == "" {
		return
	}

	if out, err := exec.Command("git", "init", "--bare", "-q", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare %s: %v\n%s", remote, err, out)
	}

	run(t, path, "remote", "add", "origin", remote)
	run(t, path, "push", "-q", "origin", "main")
	run(t, path, "fetch", "-q", "origin")
}

func codebase(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed, so a real folder cannot be built to snapshot")
	}

	base := t.TempDir()
	root := filepath.Join(base, "work")
	remotes := filepath.Join(base, "remotes")

	if err := os.MkdirAll(remotes, 0o755); err != nil {
		t.Fatalf("create %s: %v", remotes, err)
	}

	source := filepath.Join(base, "source", "ui")
	started(t, source, "")

	started(t, filepath.Join(root, "norn"), filepath.Join(remotes, "norn.git"))
	started(t, filepath.Join(root, "runner"), filepath.Join(remotes, "runner.git"))
	started(t, filepath.Join(root, "norn", "scratch"), "")

	run(t, filepath.Join(root, "norn"),
		"-c", "protocol.file.allow=always", "submodule", "--quiet", "add", source, "web/ui")
	run(t, filepath.Join(root, "norn"), "commit", "-q", "-m", "add ui")
	run(t, filepath.Join(root, "norn"), "push", "-q", "origin", "main")
	run(t, filepath.Join(root, "norn"), "fetch", "-q", "origin")
	run(t, filepath.Join(root, "norn"), "worktree", "add", "--detach", "-q", "../wt", "HEAD")

	if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatalf("create archive: %v", err)
	}

	bare := filepath.Join(root, "archive", "old.git")

	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	writeFile(t, filepath.Join(root, "AGENTS.md"), "rules\n")
	writeFile(t, filepath.Join(root, "Makefile"), "all:\n")
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "read me\n")
	writeFile(t, filepath.Join(root, ".env"), "SECRET=hunter2\n")
	writeFile(t, filepath.Join(root, "node_modules", "left-pad", "index.js"), "1\n")

	return root
}

const snapshotBudget = 30 * time.Second

type engine struct {
	t       *testing.T
	root    string
	dir     *statedir.Dir
	service service.Snapshots
}

func newEngine(t *testing.T) *engine {
	t.Helper()

	root := codebase(t)

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("create the state directory: %v", err)
	}

	held := scanned(t, root)

	inventories := inventoryrepo.New(dir)
	if err := inventories.Save(context.Background(), held); err != nil {
		t.Fatalf("record the codebase: %v", err)
	}

	return &engine{
		t:    t,
		root: root,
		dir:  dir,
		service: snapshotsvc.New(
			worktreerepo.New(defaults(), results()),
			materialiserrepo.New(),
			settingsrepo.New(),
			inventories,
			runrepo.New(dir),
			defaults(),
		),
	}
}

func scanned(t *testing.T, root string) entity.Codebase {
	t.Helper()

	folder, err := scannerrepo.New(config.Codebase{
		ScanDepth:    entity.ScanDepthDefault,
		ProbeTimeout: defaults().GitTimeout,
	}).Scan(context.Background(), root, entity.ScanDepthDefault)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}

	inventory := entity.Inventory{
		Name:         filepath.Base(folder.Root),
		RootPath:     folder.Root,
		Repositories: entity.Classify(folder.Root, folder.Repositories),
		SharedFiles:  folder.SharedFiles,
	}

	return entity.Codebase{
		ID:        uuid.New(),
		Name:      inventory.Name,
		RootPath:  folder.Root,
		Confirmed: inventory,
		Reported:  inventory,
	}
}

func (e *engine) take(key string, attempt int, dirty bool) entity.Snapshot {
	e.t.Helper()

	request := service.TakeRequest{Path: e.root, IssueKey: key, Attempt: attempt}

	if dirty {
		request.LocalChanges = entity.LocalChangesInclude
	}

	taken, err := e.service.Take(context.Background(), request)
	if err != nil {
		e.t.Fatalf("snapshot %s: %v", e.root, err)
	}

	return taken
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

		if entry.Name() == ".git" {
			if entry.IsDir() {
				return fs.SkipDir
			}

			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		switch {
		case entry.IsDir():
			tree[relative] = "dir|" + info.Mode().String()
		case entry.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}

			tree[relative] = "link|" + target
		default:
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			sum := sha256.Sum256(body)
			tree[relative] = fmt.Sprintf(
				"%s|%d|%s|%s",
				info.Mode(), info.Size(), info.ModTime().UTC(), hex.EncodeToString(sum[:]),
			)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("fingerprint %s: %v", root, err)
	}

	return tree
}

func TestAFolderOfRepositoriesIsSnapshottedInSecondsAndLeavesTheOriginalsAlone(t *testing.T) {
	e := newEngine(t)
	before := fingerprint(t, e.root)

	taken := e.take("NORN-46", 1, false)

	if taken.Took > snapshotBudget {
		t.Fatalf("the snapshot took %s; worktrees share the object store precisely so that this "+
			"is seconds and not minutes", taken.Took)
	}

	wanted := map[string]string{
		"norn":         "norn/NORN-46/norn",
		"norn/scratch": "norn/NORN-46/scratch",
		"runner":       "norn/NORN-46/runner",
		"wt":           "norn/NORN-46/wt",
	}

	if len(taken.Repositories) != len(wanted) {
		t.Fatalf("%d repositories were snapshotted and the folder holds %d that can be worked in: %+v",
			len(taken.Repositories), len(wanted), taken.Repositories)
	}

	for _, held := range taken.Repositories {
		branch, known := wanted[held.RelPath]
		if !known {
			t.Fatalf("%s was snapshotted and should not have been", held.RelPath)
		}

		if held.Branch != branch {
			t.Fatalf("%s is on %q and should be on %q", held.RelPath, held.Branch, branch)
		}

		if got := run(t, held.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != branch {
			t.Fatalf("%s on disk is on %q", held.RelPath, got)
		}
	}

	if _, err := os.Stat(filepath.Join(taken.Workspace, "archive", "old.git")); err == nil {
		t.Fatalf("a bare repository was copied into the snapshot as loose files")
	}

	untouched(t, before, fingerprint(t, e.root))
}

func untouched(t *testing.T, before, after map[string]string) {
	t.Helper()

	for path, want := range before {
		got, present := after[path]

		switch {
		case !present:
			t.Fatalf("%s disappeared while the snapshot was taken", path)
		case got != want:
			t.Fatalf(
				"%s changed while the snapshot was taken (%s became %s); a person's own working "+
					"copies are the one thing this may never write to",
				path, want, got,
			)
		}
	}

	for path := range after {
		if _, present := before[path]; !present {
			t.Fatalf("taking the snapshot created %s inside the person's folder", path)
		}
	}
}

func TestASecretInTheFolderNeverReachesTheSnapshotHoweverItIsAskedFor(t *testing.T) {
	e := newEngine(t)

	writeFile(t, filepath.Join(e.root, entity.IgnoreFileName), "!.env\n!*.pem\n!.norn/\n")

	taken := e.take("NORN-46", 1, false)

	if _, err := os.Stat(filepath.Join(taken.Workspace, ".env")); err == nil {
		t.Fatalf(
			"the .env reached the snapshot even though the built-in denylist covers it; a %s that "+
				"can re-include a secret is a %s that hands one to a coding agent",
			entity.IgnoreFileName, entity.IgnoreFileName,
		)
	}

	for _, file := range taken.Shared {
		if file.RelPath == ".env" {
			t.Fatalf("the snapshot records having copied the .env")
		}
	}
}

func TestTheSnapshotCarriesTheSharedFilesAndLeavesTheCachesBehind(t *testing.T) {
	e := newEngine(t)
	taken := e.take("NORN-46", 1, false)

	for _, relPath := range []string{"AGENTS.md", "Makefile", filepath.Join("docs", "guide.md")} {
		if _, err := os.Stat(filepath.Join(taken.Workspace, relPath)); err != nil {
			t.Fatalf(
				"%s is missing from the snapshot: %v; an execution reads the folder's own "+
					"instructions before it does anything else",
				relPath, err,
			)
		}
	}

	if _, err := os.Stat(filepath.Join(taken.Workspace, "node_modules")); err == nil {
		t.Fatalf("node_modules was copied into the snapshot")
	}
}

func TestUncommittedWorkArrivesAsOneCommitAndTheSnapshotIsClean(t *testing.T) {
	e := newEngine(t)

	writeFile(t, filepath.Join(e.root, "runner", "README.md"), "hello, and more\n")
	writeFile(t, filepath.Join(e.root, "runner", "notes.md"), "not committed yet\n")
	writeFile(t, filepath.Join(e.root, "runner", ".env"), "SECRET=hunter2\n")
	writeFile(t, filepath.Join(e.root, "runner", "debug.log"), "noise\n")

	taken := e.take("NORN-46", 1, true)

	held := repositoryAt(t, taken, "runner")

	if held.Local == nil {
		t.Fatalf("the uncommitted work in runner was not carried across")
	}

	if status := run(t, held.Path, "status", "--porcelain"); status != "" {
		t.Fatalf(
			"the snapshot is dirty after carrying local work across:\n%s\nthe coding agent has to "+
				"start from a clean tree or its own diff becomes unreadable",
			status,
		)
	}

	if got := run(t, held.Path, "log", "-1", "--format=%s"); !strings.HasPrefix(got, "norn: local changes at ") {
		t.Fatalf("the top commit of the snapshot reads %q", got)
	}

	if body, err := os.ReadFile(filepath.Join(held.Path, "README.md")); err != nil || string(body) != "hello, and more\n" {
		t.Fatalf("the tracked change did not arrive: %q, %v", body, err)
	}

	if _, err := os.Stat(filepath.Join(held.Path, "notes.md")); err != nil {
		t.Fatalf("the untracked file did not arrive: %v", err)
	}

	if _, err := os.Stat(filepath.Join(held.Path, ".env")); err == nil {
		t.Fatalf(
			"an uncommitted .env was committed onto the execution's branch; that branch gets " +
				"pushed, so this would publish a secret",
		)
	}

	if _, err := os.Stat(filepath.Join(held.Path, "debug.log")); err == nil {
		t.Fatalf("an untracked log file was committed onto the execution's branch")
	}
}

func TestDiscardingASnapshotLeavesNoWorktreeRegistrationBehind(t *testing.T) {
	e := newEngine(t)

	norn := filepath.Join(e.root, "norn")
	before := len(strings.Split(run(t, norn, "worktree", "list"), "\n"))

	taken := e.take("NORN-46", 1, false)

	if during := len(strings.Split(run(t, norn, "worktree", "list"), "\n")); during <= before {
		t.Fatalf("the snapshot registered no worktrees at all")
	}

	if err := e.service.Discard(context.Background(), taken.Name); err != nil {
		t.Fatalf("discard %s: %v", taken.Name, err)
	}

	after := strings.Split(run(t, norn, "worktree", "list"), "\n")

	if len(after) != before {
		t.Fatalf(
			"the repository still lists %d worktrees and started with %d:\n%s\na registration "+
				"left behind makes the person's own repository complain about a folder that is gone",
			len(after), before, strings.Join(after, "\n"),
		)
	}

	if _, err := os.Stat(taken.Run); err == nil {
		t.Fatalf("%s is still on disk after it was discarded", taken.Run)
	}
}

func TestASubmoduleThatCannotBeFilledInIsNamedRatherThanLeftSilentlyEmpty(t *testing.T) {
	e := newEngine(t)
	taken := e.take("NORN-46", 1, false)

	held := repositoryAt(t, taken, "norn")

	if _, err := os.Stat(filepath.Join(held.Path, "web", "ui", "README.md")); err == nil {
		return
	}

	named := false

	for _, warning := range taken.Warnings {
		named = named || (strings.Contains(warning, "norn") && strings.Contains(warning, "submodule"))
	}

	if !named {
		t.Fatalf(
			"the submodule under norn is empty and nothing said so; a coding agent handed a "+
				"folder that should hold a dependency and does not will spend its run guessing "+
				"why the build fails: %v",
			taken.Warnings,
		)
	}
}

func repositoryAt(t *testing.T, snapshot entity.Snapshot, relPath string) entity.SnapshotRepository {
	t.Helper()

	for _, held := range snapshot.Repositories {
		if held.RelPath == relPath {
			return held
		}
	}

	t.Fatalf("%s is not in the snapshot", relPath)

	return entity.SnapshotRepository{}
}

func TestARunOfItsOwnIsFilledInPlaceRatherThanGettingASecondDirectory(t *testing.T) {
	e := newEngine(t)

	taken, err := e.service.Take(context.Background(), service.TakeRequest{
		Path:     e.root,
		IssueKey: "NORN-47",
		Attempt:  1,
		Run:      "exec-01ABC",
	})
	if err != nil {
		t.Fatalf("snapshot into a run of its own: %v", err)
	}

	if taken.Name != "exec-01ABC" || taken.Run != e.dir.Run("exec-01ABC") {
		t.Fatalf("the snapshot landed in %q as %q", taken.Run, taken.Name)
	}

	if _, err := os.Stat(e.dir.Run(entity.RunNameFor("NORN-47", 1))); !os.IsNotExist(err) {
		t.Fatalf("a second directory was made beside the run: %v", err)
	}

	held, err := runrepo.New(e.dir).Load(context.Background(), "exec-01ABC")
	if err != nil {
		t.Fatalf("read what the run copied: %v", err)
	}

	if len(held.Repositories) != len(taken.Repositories) {
		t.Fatalf("the run remembers %d repositories, want %d",
			len(held.Repositories), len(taken.Repositories))
	}
}

func TestASecondAttemptTakesUpTheBranchTheFirstOneCommittedOn(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	first, err := e.service.Take(ctx, service.TakeRequest{
		Path: e.root, IssueKey: "NORN-47", Attempt: 1, Run: "exec-01ABC",
	})
	if err != nil {
		t.Fatalf("snapshot the first attempt: %v", err)
	}

	held := repositoryAt(t, first, "runner")

	writeFile(t, filepath.Join(held.Path, "carried.txt"), "work from the first attempt\n")
	run(t, held.Path, "add", "carried.txt")
	run(t, held.Path, "commit", "-q", "-m", "carried")

	if err := e.service.Release(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("give the first attempt's workspace back: %v", err)
	}

	second, err := e.service.Take(ctx, service.TakeRequest{
		Path:     e.root,
		IssueKey: "NORN-47",
		Attempt:  2,
		Run:      "exec-01DEF",
		Branches: map[string]string{"runner": held.Branch},
	})
	if err != nil {
		t.Fatalf("snapshot the second attempt: %v", err)
	}

	again := repositoryAt(t, second, "runner")

	if again.Branch != held.Branch {
		t.Fatalf("the second attempt is on %q, want the first attempt's %q", again.Branch, held.Branch)
	}

	if _, err := os.Stat(filepath.Join(again.Path, "carried.txt")); err != nil {
		t.Fatalf("the work the first attempt committed is not in the second: %v", err)
	}
}

func TestGivingAWorkspaceBackReleasesTheWorktreesAndKeepsTheRecord(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	taken, err := e.service.Take(ctx, service.TakeRequest{
		Path: e.root, IssueKey: "NORN-47", Attempt: 1, Run: "exec-01ABC",
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	held := repositoryAt(t, taken, "runner")

	if err := e.service.Release(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("release: %v", err)
	}

	if _, err := os.Stat(taken.Workspace); !os.IsNotExist(err) {
		t.Fatalf("the workspace is still there: %v", err)
	}

	if listed := run(t, held.Source, "worktree", "list"); strings.Contains(listed, taken.Workspace) {
		t.Fatalf("the original still has the run's worktree registered:\n%s", listed)
	}

	if branches := run(t, held.Source, "branch", "--list"); !strings.Contains(branches, held.Branch) {
		t.Fatalf("the branch went with the workspace; it is the work:\n%s", branches)
	}

	if _, err := runrepo.New(e.dir).Load(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("the record of what the run copied went with the workspace: %v", err)
	}
}

func TestARunHandedToTheEngineKeepsWhatExplainsItWhenTheCopyFails(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	if _, err := runrepo.New(e.dir).Open(ctx, "exec-01ABC"); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(e.root, "runner", ".git")); err != nil {
		t.Fatalf("break a repository: %v", err)
	}

	if _, err := e.service.Take(ctx, service.TakeRequest{
		Path: e.root, IssueKey: "NORN-47", Attempt: 1, Run: "exec-01ABC",
	}); err == nil {
		t.Fatalf("a folder with a broken repository was copied without complaint")
	}

	if _, err := os.Stat(filepath.Join(e.dir.Run("exec-01ABC"), entity.RunWorkspaceDir)); !os.IsNotExist(err) {
		t.Fatalf("a half-built workspace was left behind: %v", err)
	}

	for _, child := range []string{entity.RunMetadataDir, entity.RunLogsDir} {
		if _, err := os.Stat(filepath.Join(e.dir.Run("exec-01ABC"), child)); err != nil {
			t.Fatalf("%s went with the failed copy, so nothing explains it any more: %v", child, err)
		}
	}
}

func lone(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed, so a real folder cannot be built to snapshot")
	}

	root := filepath.Join(t.TempDir(), "greeter")

	started(t, root, "")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "read me\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "second")

	return root
}

func newLoneEngine(t *testing.T) *engine {
	t.Helper()

	root := lone(t)

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("create the state directory: %v", err)
	}

	inventories := inventoryrepo.New(dir)

	if err := inventories.Save(context.Background(), scanned(t, root)); err != nil {
		t.Fatalf("record the codebase: %v", err)
	}

	return &engine{
		t:    t,
		root: root,
		dir:  dir,
		service: snapshotsvc.New(
			worktreerepo.New(defaults(), results()),
			materialiserrepo.New(),
			settingsrepo.New(),
			inventories,
			runrepo.New(dir),
			defaults(),
		),
	}
}

func TestAFolderThatIsItselfOneRepositoryIsSnapshottedIntoAWorkspaceOfItsOwn(t *testing.T) {
	e := newLoneEngine(t)
	before := fingerprint(t, e.root)

	taken := e.take("NORN-69", 1, false)

	if len(taken.Repositories) != 1 {
		t.Fatalf("a folder that is one repository was snapshotted as %+v", taken.Repositories)
	}

	held := taken.Repositories[0]

	if held.RelPath != "." {
		t.Fatalf("the repository at the root of the folder came back at %q", held.RelPath)
	}

	if held.Branch != "norn/NORN-69/greeter" {
		t.Fatalf("the repository is on %q", held.Branch)
	}

	for _, name := range []string{"main.go", "README.md", filepath.Join("docs", "guide.md")} {
		if _, err := os.Stat(filepath.Join(taken.Workspace, name)); err != nil {
			t.Fatalf("%s is not in the workspace: %v", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join(taken.Workspace, ".git")); err != nil {
		t.Fatalf("the workspace has no git of its own: %v", err)
	}

	if len(taken.Shared) != 0 {
		t.Fatalf(
			"a folder with nothing outside its repository carried %+v across as shared files",
			taken.Shared,
		)
	}

	untouched(t, before, fingerprint(t, e.root))
}

func TestUncommittedWorkInAFolderThatIsItselfOneRepositoryIsCarriedAcrossAndNamed(t *testing.T) {
	e := newLoneEngine(t)

	writeFile(t, filepath.Join(e.root, "README.md"), "hello, and more\n")
	writeFile(t, filepath.Join(e.root, "notes.md"), "not committed yet\n")

	taken := e.take("NORN-69", 1, true)

	held := taken.Repositories[0]

	if held.Local == nil || held.Local.PatchFile == "" {
		t.Fatalf("the uncommitted work in the folder was not carried across: %+v", held.Local)
	}

	name := filepath.Base(held.Local.PatchFile)

	if strings.HasPrefix(name, ".") {
		t.Fatalf("the patch for the folder's own repository was filed as %q, which is hidden", name)
	}

	if _, err := os.Stat(filepath.Join(taken.Run, entity.RunMetadataDir, held.Local.PatchFile)); err != nil {
		t.Fatalf("the patch is not where the snapshot says it is: %v", err)
	}

	if status := run(t, held.Path, "status", "--porcelain"); status != "" {
		t.Fatalf("the snapshot is dirty after carrying local work across:\n%s", status)
	}

	if _, err := os.Stat(filepath.Join(held.Path, "notes.md")); err != nil {
		t.Fatalf("the untracked file did not arrive: %v", err)
	}
}

func results() config.Results {
	return config.Results{
		CreatePRs:    config.PullRequestsAuto,
		Attribution:  config.AttributionNone,
		PushTimeout:  60 * time.Second,
		ForgeTimeout: 30 * time.Second,
		MaxDiffBytes: 3 << 20,
	}
}
