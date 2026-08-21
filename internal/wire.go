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
	credentialrepo "github.com/usenorn/runner/internal/repository/credential"
	dashboardrepo "github.com/usenorn/runner/internal/repository/dashboard"
	identityrepo "github.com/usenorn/runner/internal/repository/identity"
	releaserepo "github.com/usenorn/runner/internal/repository/release"
	enrolmentsvc "github.com/usenorn/runner/internal/service/enrolment"
	sessionsvc "github.com/usenorn/runner/internal/service/session"
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

	sessionsvc.Set,
	enrolmentsvc.Set,
	updatesvc.Set,

	control.Set,
	wire.Bind(new(http.Handler), new(*control.Server)),

	NewDaemon,
	NewStatus,
	NewVersion,
	NewBinding,
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

func InitInstaller(cfgFile string, overrides config.Overrides) (*Installer, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}
