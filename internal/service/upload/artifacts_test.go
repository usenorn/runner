package upload_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/entity"
)

func (h *harness) workspace(t *testing.T, executionID string) string {
	t.Helper()

	ctx := context.Background()

	if _, err := h.runs.Open(ctx, executionID); err != nil {
		t.Fatalf("make a run directory: %v", err)
	}

	if err := h.runs.SaveTask(ctx, entity.Execution{
		ID:        executionID,
		Reference: "NORN-52",
		IssueKey:  "NORN-52",
		Attempt:   1,
		Directory: h.dir.Run(executionID),
		State:     channelv1.StateRunning,
	}); err != nil {
		t.Fatalf("write a task: %v", err)
	}

	return filepath.Join(h.dir.Run(executionID), entity.RunWorkspaceDir)
}

func (h *harness) takesArtifacts(t *testing.T) *[][]byte {
	t.Helper()

	taken := &[][]byte{}

	h.posts.EXPECT().
		PublishArtifact(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context,
			_ string,
			_ string,
			label string,
			body io.Reader,
		) (entity.ArtifactReceipt, error) {
			raw, err := io.ReadAll(body)
			if err != nil {
				return entity.ArtifactReceipt{}, err
			}

			*taken = append(*taken, raw)

			return entity.ArtifactReceipt{
				ID: "art-01", Label: label, Bytes: int64(len(raw)),
			}, nil
		}).
		AnyTimes()

	return taken
}

func TestAFileFromTheRunsWorkspaceIsKeptWithTheRunAndSentToNorn(t *testing.T) {
	h := newHarness(t, settings())
	taken := h.takesArtifacts(t)

	workspace := h.workspace(t, "exec-01ART")

	if err := os.WriteFile(
		filepath.Join(workspace, "report.md"), []byte("# what changed\n"), 0o600,
	); err != nil {
		t.Fatalf("write a file in the workspace: %v", err)
	}

	receipt, err := h.service.Publish(context.Background(), "exec-01ART", entity.Artifact{
		Path:  "report.md",
		Label: "what changed",
	})
	if err != nil {
		t.Fatalf("publish a file the run produced: %v", err)
	}

	if receipt.ID == "" {
		t.Fatalf("nothing came back to reference the file by, so nothing can point at it")
	}

	if len(*taken) != 1 || string((*taken)[0]) != "# what changed\n" {
		t.Fatalf("norn was sent %q, not the file the run named", *taken)
	}

	kept := filepath.Join(h.dir.Run("exec-01ART"), entity.RunArtifactsDir, "report.md")

	if _, err := os.Stat(kept); err != nil {
		t.Fatalf(
			"the file was sent but not kept beside the run: %v. The workspace is deleted at "+
				"teardown, so a copy that only lived there is gone by the time anybody looks",
			err,
		)
	}
}

func TestAFileOutsideTheRunsWorkspaceCannotBePublished(t *testing.T) {
	h := newHarness(t, settings())
	h.takesArtifacts(t)

	h.workspace(t, "exec-01OUT")

	outside := filepath.Join(t.TempDir(), "secrets.env")

	if err := os.WriteFile(outside, []byte("TOKEN=hunter2\n"), 0o600); err != nil {
		t.Fatalf("write a file outside the workspace: %v", err)
	}

	_, err := h.service.Publish(context.Background(), "exec-01OUT", entity.Artifact{
		Path:  outside,
		Label: "secrets",
	})

	if !errors.Is(err, entity.ErrArtifactOutside) {
		t.Fatalf(
			"a run uploaded %s to norn: %v. Publishing is the one tool that moves bytes off "+
				"this machine, and it may only ever move bytes the run itself made",
			outside, err,
		)
	}
}

func TestALinkOutOfTheWorkspaceIsNotAWayAroundIt(t *testing.T) {
	h := newHarness(t, settings())
	h.takesArtifacts(t)

	workspace := h.workspace(t, "exec-01LINK")

	outside := filepath.Join(t.TempDir(), "secrets.env")

	if err := os.WriteFile(outside, []byte("TOKEN=hunter2\n"), 0o600); err != nil {
		t.Fatalf("write a file outside the workspace: %v", err)
	}

	if err := os.Symlink(outside, filepath.Join(workspace, "innocent.md")); err != nil {
		t.Fatalf("link to it from inside the workspace: %v", err)
	}

	_, err := h.service.Publish(context.Background(), "exec-01LINK", entity.Artifact{
		Path:  "innocent.md",
		Label: "notes",
	})

	if !errors.Is(err, entity.ErrArtifactOutside) {
		t.Fatalf(
			"a link inside the workspace carried a file from outside it to norn: %v. A path "+
				"has to be followed all the way before it is compared, or the check is one an "+
				"'ln -s' walks past",
			err,
		)
	}
}

func TestAFileLargerThanNornTakesIsRefusedByName(t *testing.T) {
	cfg := settings()
	cfg.MaxArtifactBytes = 16

	h := newHarness(t, cfg)
	h.takesArtifacts(t)

	workspace := h.workspace(t, "exec-01BIG")

	if err := os.WriteFile(
		filepath.Join(workspace, "dump.bin"), make([]byte, 4096), 0o600,
	); err != nil {
		t.Fatalf("write a large file: %v", err)
	}

	_, err := h.service.Publish(context.Background(), "exec-01BIG", entity.Artifact{
		Path:  "dump.bin",
		Label: "a dump",
	})

	if !errors.Is(err, entity.ErrArtifactTooLarge) {
		t.Fatalf("a file past the cap was sent anyway: %v", err)
	}
}

func TestPublishingSomethingThatIsNotThereSaysSoRatherThanFailingLater(t *testing.T) {
	h := newHarness(t, settings())
	h.takesArtifacts(t)

	h.workspace(t, "exec-01NONE")

	_, err := h.service.Publish(context.Background(), "exec-01NONE", entity.Artifact{
		Path:  "never-written.md",
		Label: "notes",
	})

	if !errors.Is(err, entity.ErrArtifactMissing) {
		t.Fatalf("publishing a file that does not exist answered %v", err)
	}
}
