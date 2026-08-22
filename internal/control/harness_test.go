package control_test

import (
	"net/http"
	"os"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	capabilityrepo "github.com/usenorn/runner/internal/repository/capability"
	credentialrepo "github.com/usenorn/runner/internal/repository/credential"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
	identityrepo "github.com/usenorn/runner/internal/repository/identity"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
	releaserepo "github.com/usenorn/runner/internal/repository/release"
	scannerrepo "github.com/usenorn/runner/internal/repository/scanner"
	codebasesvc "github.com/usenorn/runner/internal/service/codebase"
	enrolmentsvc "github.com/usenorn/runner/internal/service/enrolment"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
	updatesvc "github.com/usenorn/runner/internal/service/update"
)

type harness struct {
	dir         *statedir.Dir
	client      *control.Client
	build       entity.Build
	dashboard   *dashboardrepo.MockDashboard
	credentials *credentialrepo.MockCredential
	identities  repository.Identity
}

func codebaseSettings() config.Codebase {
	return config.Codebase{
		ScanDepth:      entity.ScanDepthDefault,
		RescanInterval: time.Hour,
		ProbeTimeout:   5 * time.Second,
	}
}

func sessionSettings() config.Session {
	return config.Session{
		RequestTimeout: time.Second,
		RefreshLead:    time.Minute,
		RetryMin:       time.Minute,
		RetryMax:       time.Minute,
	}
}

func updateSettings() config.Update {
	return config.Update{
		Check:    true,
		Interval: time.Hour,
		Timeout:  time.Second,
		Feed:     "https://releases.example/latest",
	}
}

func settings() config.Control {
	return config.Control{
		DialTimeout:       time.Second,
		RequestTimeout:    2 * time.Second,
		ReadHeaderTimeout: time.Second,
		ShutdownTimeout:   3 * time.Second,
	}
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

func newHarness(t *testing.T, handler http.Handler) *harness {
	t.Helper()

	dir := newStateDir(t)

	listener, cleanup, err := socket.New(dir)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctrl := gomock.NewController(t)

	dashboard := dashboardrepo.NewMockDashboard(ctrl)
	credentials := credentialrepo.NewMockCredential(ctrl)
	identities := identityrepo.New(dir)

	releases := releaserepo.NewMockRelease(ctrl)
	releases.EXPECT().Latest(gomock.Any()).Return(entity.Release{Version: "v9.9.9"}, nil).AnyTimes()

	build := entity.Build{Version: "v1.0.0", OS: "darwin", Arch: "arm64", Go: "go1.26.6"}
	updates := updatesvc.New(releases, build, updateSettings())

	sessions := sessionsvc.New(dashboard, identities, credentials, sessionSettings())
	enrolments := enrolmentsvc.New(
		dashboard,
		identities,
		credentials,
		sessions,
		entity.Host{Hostname: "test-box", OS: "darwin", Arch: "arm64", Version: "test"},
	)

	codebases := codebasesvc.New(
		scannerrepo.New(codebaseSettings()),
		capabilityrepo.New(codebaseSettings()),
		inventoryrepo.New(dir),
		dashboard,
		sessions,
		codebaseSettings(),
	)

	if handler == nil {
		handler = control.NewServer(
			config.Runner{Server: "https://norn.example", Capacity: 4, Runtime: config.RuntimeAuto},
			config.State{Root: dir.Root(), ConfigFile: dir.Config()},
			config.App{Version: "test"},
			dir,
			enrolments,
			sessions,
			updates,
			codebases,
			build,
		)
	}

	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}

	go func() { _ = server.Serve(listener) }()

	t.Cleanup(func() {
		_ = server.Close()
		cleanup()
	})

	return &harness{
		dir:         dir,
		build:       build,
		client:      control.NewClient(settings(), dir),
		dashboard:   dashboard,
		credentials: credentials,
		identities:  identities,
	}
}
