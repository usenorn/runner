package buildinfo

import (
	"runtime"
	"runtime/debug"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
)

const (
	revisionKey = "vcs.revision"
	timeKey     = "vcs.time"
	modifiedKey = "vcs.modified"
)

func New(app config.App) entity.Build {
	build := entity.Build{
		Version: app.Version,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Go:      runtime.Version(),
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return build
	}

	for _, setting := range info.Settings {
		switch setting.Key {
		case revisionKey:
			build.Commit = setting.Value
		case timeKey:
			if stamped, err := time.Parse(time.RFC3339, setting.Value); err == nil {
				build.CommittedAt = stamped
			}
		case modifiedKey:
			build.Modified = setting.Value == "true"
		}
	}

	return build
}
