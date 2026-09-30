package upload_test

import (
	"os"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	uploadrepo "github.com/usenorn/runner/internal/repository/upload"
	"github.com/usenorn/runner/internal/service"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
	uploadsvc "github.com/usenorn/runner/internal/service/upload"
)

type harness struct {
	dir      *statedir.Dir
	runs     repository.Run
	posts    *uploadrepo.MockUpload
	sessions *sessionsvc.MockSessions
	service  service.Uploads
}

func newHarness(t *testing.T, cfg config.Upload) *harness {
	t.Helper()

	controller := gomock.NewController(t)

	dir := newStateDir(t)

	h := &harness{
		dir:      dir,
		runs:     runrepo.New(dir),
		posts:    uploadrepo.NewMockUpload(controller),
		sessions: sessionsvc.NewMockSessions(controller),
	}

	h.sessions.EXPECT().Access(gomock.Any()).Return("access-token", nil).AnyTimes()

	h.service = uploadsvc.New(h.posts, h.runs, h.sessions, cfg)

	return h
}

func settings() config.Upload {
	return config.Upload{MaxArtifactBytes: 1 << 20}
}

func newStateDir(t *testing.T) *statedir.Dir {
	t.Helper()

	root, err := os.MkdirTemp("/tmp", "nrn")
	if err != nil {
		t.Fatalf("create temporary root: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(root) })

	dir, err := statedir.New(config.State{Root: root})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	return dir
}
