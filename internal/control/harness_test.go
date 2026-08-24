package control_test

import (
	"context"
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
	runtokenrepo "github.com/usenorn/runner/internal/repository/runtoken"
	scannerrepo "github.com/usenorn/runner/internal/repository/scanner"
	schedulingrepo "github.com/usenorn/runner/internal/repository/scheduling"
	servicelogrepo "github.com/usenorn/runner/internal/repository/servicelog"
	settingsrepo "github.com/usenorn/runner/internal/repository/settings"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	worktreerepo "github.com/usenorn/runner/internal/repository/worktree"
	channelsvc "github.com/usenorn/runner/internal/service/channel"
	codebasesvc "github.com/usenorn/runner/internal/service/codebase"
	enrolmentsvc "github.com/usenorn/runner/internal/service/enrolment"
	executionsvc "github.com/usenorn/runner/internal/service/execution"
	previewsvc "github.com/usenorn/runner/internal/service/preview"
	questionsvc "github.com/usenorn/runner/internal/service/question"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
	snapshotsvc "github.com/usenorn/runner/internal/service/snapshot"
	supervisorsvc "github.com/usenorn/runner/internal/service/supervisor"
	updatesvc "github.com/usenorn/runner/internal/service/update"
	uploadsvc "github.com/usenorn/runner/internal/service/upload"
)

type harness struct {
	dir         *statedir.Dir
	client      *control.Client
	tokens      repository.RunToken
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

	questions := questionsvc.New(runrepo.New(dir), spool, questionSettings())

	supervisorUploads := uploadsvc.NewMockUploads(ctrl)
	supervisorUploads.EXPECT().
		Line(gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes()

	services := supervisorsvc.New(
		processrepo.New(),
		portrepo.New(config.Runner{PortRange: [2]int{45100, 45199}}),
		servicelogrepo.New(dir),
		runrepo.New(dir),
		spool,
		supervisorUploads,
		supervisorSettings(),
	)

	previews := previewsvc.New(runrepo.New(dir), spool)
	tokens := runtokenrepo.New()

	executions := executionsvc.New(
		runrepo.New(dir),
		spool,
		disks,
		schedulingrepo.New(dir),
		settingsrepo.New(),
		inventoryrepo.New(dir),
		snapshotsvc.New(
			worktreerepo.New(snapshotSettings(), results()),
			materialiserrepo.New(),
			settingsrepo.New(),
			inventoryrepo.New(dir),
			runrepo.New(dir),
			snapshotSettings(),
		),
		services,
		uploadStub{},
		questions,
		previews,
		sessionStub{},
		changesetStub{},
		tokens,
		driverStub{},
		dir,
		config.Runner{Capacity: 2, Retention: keeping()},
		config.App{Version: "test"},
		config.Scheduler{},
		config.Driver{
			Profile:        config.ProfileStandard,
			ProbeTimeout:   time.Second,
			SessionTimeout: time.Minute,
			StopGrace:      time.Second,
		},
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
		questions,
		channelSettings(),
		spoolSettings(),
		config.App{Version: "test"},
	)

	if handler == nil {
		handler = control.NewServer(
			config.Runner{
				Server:    "https://norn.example",
				Capacity:  4,
				Runtime:   config.RuntimeAuto,
				Retention: keeping(),
			},
			config.State{Root: dir.Root(), ConfigFile: dir.Config()},
			config.App{Version: "test"},
			dir,
			enrolments,
			sessions,
			updates,
			codebases,
			channels,
			tunnelStub{},
			executions,
			services,
			questions,
			previews,
			uploadStub{},
			tokens,
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
		tokens:      tokens,
		client:      control.NewClient(settings(), questionSettings(), dir, ""),
		dashboard:   dashboard,
		credentials: credentials,
		identities:  identities,
	}
}

func (h *harness) bearer(t *testing.T, executionID string) control.Bearer {
	t.Helper()

	minted, err := h.tokens.Mint(context.Background(), executionID)
	if err != nil {
		t.Fatalf("mint a token for %s: %v", executionID, err)
	}

	return control.Bearer(minted)
}

func (h *harness) as(t *testing.T, executionID string) *control.Client {
	t.Helper()

	return control.NewClient(settings(), questionSettings(), h.dir, h.bearer(t, executionID))
}

type tunnelStub struct{}

func (tunnelStub) Run(context.Context) {}

func (tunnelStub) Wake() {}

func (tunnelStub) Report() entity.TunnelReport {
	return entity.TunnelReport{State: entity.TunnelOff}
}

type uploadStub struct{}

func (uploadStub) Publish(
	context.Context,
	string,
	entity.Artifact,
) (entity.ArtifactReceipt, error) {
	return entity.ArtifactReceipt{}, nil
}

func (uploadStub) Run(context.Context) {}

func (uploadStub) Open(context.Context, string) (entity.TelemetryMode, error) {
	return entity.TelemetryFull, nil
}

func (uploadStub) Event(context.Context, string, entity.DriverEvent) {}

func (uploadStub) Line(context.Context, string, entity.LogLine) {}

func (uploadStub) Flush(context.Context, string) error { return nil }

func (uploadStub) Close(context.Context, string) {}

type driverStub struct{}

func (driverStub) Preflight(context.Context, entity.DriverKind) entity.DriverHealth {
	return entity.DriverHealth{
		Kind:      entity.DriverClaude,
		Installed: true,
		Version:   "2.1.239",
		SignedIn:  true,
		Account:   "runner@example",
	}
}

func (driverStub) Start(
	context.Context,
	entity.ExecEnv,
	entity.Task,
) (repository.Session, error) {
	return nil, entity.ErrDriverMissing
}

func (driverStub) Resume(
	context.Context,
	entity.ExecEnv,
	entity.DriverSession,
	string,
) (repository.Session, error) {
	return nil, entity.ErrDriverMissing
}

func questionSettings() config.Questions {
	return config.Questions{SoftWait: 300 * time.Millisecond, MaxWait: time.Second}
}

func keeping() config.Retention {
	return config.Retention{
		WorkspaceAfterDone: 30 * time.Minute,
		RunsMaxAge:         14 * 24 * time.Hour,
		RunsMaxDisk:        20 << 30,
		SweepInterval:      time.Hour,
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

type changesetStub struct{}

func (changesetStub) Uncommitted(
	context.Context, entity.Snapshot,
) ([]entity.UncommittedWork, error) {
	return nil, nil
}

func (changesetStub) Publish(
	context.Context, entity.Execution, entity.Snapshot, entity.Completion, []entity.PreviewLink,
) (entity.ChangeSet, error) {
	return entity.ChangeSet{}, nil
}

type sessionStub struct{}

func (sessionStub) Run(context.Context) {}

func (sessionStub) Access(context.Context) (string, error) { return "", nil }

func (sessionStub) Ticket(context.Context) (string, error) { return "", nil }

func (sessionStub) TunnelTicket(context.Context) (entity.TunnelTicket, error) {
	return entity.TunnelTicket{}, nil
}

func (sessionStub) Previews() entity.PreviewService { return entity.PreviewService{} }

func (sessionStub) Report() entity.SessionReport { return entity.SessionReport{} }

func (sessionStub) Adopt(context.Context, entity.Identity) entity.SessionReport {
	return entity.SessionReport{}
}

func (sessionStub) Forget() {}

func (uploadStub) Attach(
	context.Context,
	string,
	string,
	[]byte,
) (entity.ArtifactReceipt, error) {
	return entity.ArtifactReceipt{}, nil
}
