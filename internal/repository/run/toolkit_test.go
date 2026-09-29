package run_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/usenorn/runner/internal/entity"
	runrepo "github.com/usenorn/runner/internal/repository/run"
)

func TestAToolkitReadsBackAsItWasHandedOverAndOnlyItsOwnerCanReadIt(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	path, err := runs.Open(ctx, "exec-01TKT")
	if err != nil {
		t.Fatalf("open a run: %v", err)
	}

	handed := entity.Toolkit{
		Instructions: "Commit small.",
		Skills: []entity.ToolkitSkill{{
			Name: "release-notes", ContentHash: "4f2a", DownloadURL: "https://blobs.test/release-notes.tar.gz",
		}},
		Servers: []entity.ToolkitServer{{
			Name: "linear", Transport: entity.ToolkitHTTP, URL: "https://mcp.linear.test/mcp",
			Headers: map[string]string{"Authorization": "Bearer at-rae"},
		}},
	}

	if err := runs.SaveToolkit(ctx, "exec-01TKT", handed); err != nil {
		t.Fatalf("save the toolkit: %v", err)
	}

	read, err := runs.LoadToolkit(ctx, "exec-01TKT")
	if err != nil {
		t.Fatalf("load the toolkit: %v", err)
	}

	if !reflect.DeepEqual(read, handed) {
		t.Fatalf("read back %+v, want %+v; a resumed run would start without what it was given", read, handed)
	}

	info, err := os.Stat(filepath.Join(path, entity.RunMetadataDir, entity.RunToolkitFile))
	if err != nil {
		t.Fatalf("stat the toolkit: %v", err)
	}

	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the toolkit is %v; it holds sign-in tokens for other services", info.Mode().Perm())
	}
}

func TestARunNornHandedNoToolkitSaysSo(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Open(ctx, "exec-01NON"); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	if _, err := runs.LoadToolkit(ctx, "exec-01NON"); !errors.Is(err, entity.ErrSnapshotMissing) {
		t.Fatalf("err = %v, want the run to say nothing was written down", err)
	}
}
