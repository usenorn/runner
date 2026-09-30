package upload

import (
	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

const artifactDirMode = 0o700

type uploadsService struct {
	uploads  repository.Upload
	runs     repository.Run
	sessions service.Sessions
	cfg      config.Upload
}

func New(
	uploads repository.Upload,
	runs repository.Run,
	sessions service.Sessions,
	cfg config.Upload,
) service.Uploads {
	return &uploadsService{
		uploads:  uploads,
		runs:     runs,
		sessions: sessions,
		cfg:      cfg,
	}
}
