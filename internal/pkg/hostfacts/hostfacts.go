package hostfacts

import (
	"fmt"
	"os"
	"runtime"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
)

func New(app config.App) (entity.Host, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return entity.Host{}, fmt.Errorf("read this machine's hostname: %w", err)
	}

	return entity.Host{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Version:  app.Version,
	}, nil
}
