//go:build wireinject

package internal

import (
	"net/http"

	"github.com/goforj/wire"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/pkg/servicemanager"
	"github.com/usenorn/runner/internal/pkg/socket"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

var baseSet = wire.NewSet(
	config.Set,
	logging.Set,

	statedir.Set,
	socket.Set,
	servicemanager.Set,

	control.Set,
	wire.Bind(new(http.Handler), new(*control.Server)),

	NewDaemon,
	NewStatus,
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

func InitInstaller(cfgFile string, overrides config.Overrides) (*Installer, func(), error) {
	wire.Build(baseSet)

	return nil, nil, nil
}
