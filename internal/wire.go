//go:build wireinject

package internal

import (
	"net/http"

	"github.com/goforj/wire"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/pkg/buildinfo"
	"github.com/usenorn/runner/internal/pkg/dashboardclient"
	"github.com/usenorn/runner/internal/pkg/hostfacts"
	"github.com/usenorn/runner/internal/pkg/servicemanager"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/pkg/statedir"
	capabilityrepo "github.com/usenorn/runner/internal/repository/capability"
	channelrepo "github.com/usenorn/runner/internal/repository/channel"
	credentialrepo "github.com/usenorn/runner/internal/repository/credential"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
	diskrepo "github.com/usenorn/runner/internal/repository/disk"
	identityrepo "github.com/usenorn/runner/internal/repository/identity"
	inventoryrepo "github.com/usenorn/runner/internal/repository/inventory"
	materialiserrepo "github.com/usenorn/runner/internal/repository/materialiser"
	releaserepo "github.com/usenorn/runner/internal/repository/release"
	runrepo "github.com/usenorn/runner/internal/repository/run"
	scannerrepo "github.com/usenorn/runner/internal/repository/scanner"
	settingsrepo "github.com/usenorn/runner/internal/repository/settings"
	spoolrepo "github.com/usenorn/runner/internal/repository/spool"
	worktreerepo "github.com/usenorn/runner/internal/repository/worktree"
	channelsvc "github.com/usenorn/runner/internal/service/channel"
	codebasesvc "github.com/usenorn/runner/internal/service/codebase"
	enrolmentsvc "github.com/usenorn/runner/internal/service/enrolment"
	executionsvc "github.com/usenorn/runner/internal/service/execution"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
	snapshotsvc "github.com/usenorn/runner/internal/service/snapshot"
	updatesvc "github.com/usenorn/runner/internal/service/update"
)

var baseSet = wire.NewSet(
	config.Set,
	logging.Set,

	statedir.Set,
	socket.Set,
	servicemanager.Set,
	dashboardclient.Set,
	hostfacts.Set,
	buildinfo.Set,

	identityrepo.Set,
	credentialrepo.Set,
	dashboardrepo.Set,
	releaserepo.Set,
	scannerrepo.Set,
	capabilityrepo.Set,
	inventoryrepo.Set,
	worktreerepo.Set,
	materialiserrepo.Set,
	settingsrepo.Set,
	runrepo.Set,
	spoolrepo.Set,
	channelrepo.Set,
	diskrepo.Set,

	sessionsvc.Set,
	enrolmentsvc.Set,
	updatesvc.Set,
	codebasesvc.Set,
	snapshotsvc.Set,
	executionsvc.Set,
	channelsvc.Set,

	control.Set,
	wire.Bind(new(http.Handler), new(*control.Server)),

	NewDaemon,
	NewStatus,
	NewVersion,
	NewBinding,
	NewInspection,
	NewSnapshotting,
	NewScheduling,
	NewInstaller,
)

func InitDaemon(cfgFile string, overrides config.Overrides) (*Daemon, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}

func InitStatus(cfgFile string, overrides config.Overrides) (*Status, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}

func InitVersion(cfgFile string, overrides config.Overrides) (*Version, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}

func InitBinding(cfgFile string, overrides config.Overrides) (*Binding, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}

func InitInspection(cfgFile string, overrides config.Overrides) (*Inspection, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}

func InitSnapshotting(cfgFile string, overrides config.Overrides) (*Snapshotting, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}

func InitScheduling(cfgFile string, overrides config.Overrides) (*Scheduling, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}

func InitInstaller(cfgFile string, overrides config.Overrides) (*Installer, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}
