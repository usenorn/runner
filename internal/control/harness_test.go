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
	channelrepo "github.com/usenorn/runner/internal/repository/channel"
	credentialrepo "github.com/usenorn/runner/internal/repository/credential"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
	diskrepo "github.com/usenorn/runner/internal/repository/disk"
	identityrepo "github.com/usenorn/runner/internal/repository/identity"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
	materialiserrepo "github.com/usenorn/runner/internal/repository/materialiser"
	portrepo "github.com/usenorn/runner/internal/repository/port"
	processrepo "github.com/usenorn/runner/internal/repository/process"
	releaserepo "github.com/usenorn/runner/internal/repository/release"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	scannerrepo "github.com/usenorn/runner/internal/repository/scanner"
	servicelogrepo "github.com/usenorn/runner/internal/repository/servicelog"
	settingsrepo "github.com/usenorn/runner/internal/repository/settings"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	worktreerepo "github.com/usenorn/runner/internal/repository/worktree"
	channelsvc "github.com/usenorn/runner/internal/service/channel"
	codebasesvc "github.com/usenorn/runner/internal/service/codebase"
	enrolmentsvc "github.com/usenorn/runner/internal/service/enrolment"
	executionsvc "github.com/usenorn/runner/internal/service/execution"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
	snapshotsvc "github.com/usenorn/runner/internal/service/snapshot"
	supervisorsvc "github.com/usenorn/runner/internal/service/supervisor"
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

func channelSettings() config.Channel {
	return config.Channel{
		Enabled:          true,
		HandshakeTimeout: time.Second,
		Heartbeat:        15 * time.Second,
		WriteTimeout:     time.Second,
		RetryMin:         time.Second,
		RetryMax:         time.Minute,
		MaxMessageBytes:  1 << 20,
	}
}

func snapshotSettings() config.Snapshot {
	return config.Snapshot{
		GitMode:        string(entity.GitModeWorktree),
		Base:           string(entity.BaseOriginDefault),
		LocalChanges:   string(entity.LocalChangesExclude),
		Fetch:          false,
		FetchTimeout:   time.Second,
		GitTimeout:     time.Second,
		MaxSharedBytes: 1 << 20,
	}
}

func supervisorSettings() config.Supervisor {
	return config.Supervisor{
		HealthInterval:  20 * time.Millisecond,
		HealthTimeout:   time.Second,
		StopGrace:       time.Second,
		RestartAttempts: 1,
		RestartBackoff:  10 * time.Millisecond,
		StepTimeout:     5 * time.Second,
	}
}

func spoolSettings() config.Spool {
	return config.Spool{MaxMessages: 100, MaxAge: time.Hour, Batch: 8}
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

	spool := spoolrepo.New(dir)
	disks := diskrepo.New()

	services := supervisorsvc.New(
		processrepo.New(),
		portrepo.New(config.Runner{PortRange: [2]int{45100, 45199}}),
		servicelogrepo.New(dir),
		runrepo.New(dir),
		spool,
		supervisorSettings(),
	)

	executions := executionsvc.New(
		runrepo.New(dir),
		spool,
		disks,
		settingsrepo.New(),
		inventoryrepo.New(dir),
		snapshotsvc.New(
			worktreerepo.New(snapshotSettings()),
			materialiserrepo.New(),
			settingsrepo.New(),
			inventoryrepo.New(dir),
			runrepo.New(dir),
			snapshotSettings(),
		),
		services,
		dir,
		config.Runner{Capacity: 2},
		config.App{Version: "test"},
		config.Scheduler{},
	)

	channels := channelsvc.New(
		channelrepo.New(
			config.Runner{Server: "https://norn.example"},
			config.App{Version: "test"},
			channelSettings(),
		),
		spool,
		sessions,
		executions,
		channelSettings(),
		spoolSettings(),
		config.App{Version: "test"},
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
			channels,
			executions,
			services,
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
