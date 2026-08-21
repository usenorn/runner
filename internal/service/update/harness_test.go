package update_test

import (
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	releaserepo "github.com/usenorn/runner/internal/repository/release"
	"github.com/usenorn/runner/internal/service"
	updatesvc "github.com/usenorn/runner/internal/service/update"
)

const installed = "v1.2.0"

type harness struct {
	releases *releaserepo.MockRelease
	service  service.Updates
}

func newHarness(t *testing.T, version string, check bool) *harness {
	t.Helper()

	releases := releaserepo.NewMockRelease(gomock.NewController(t))

	return &harness{
		releases: releases,
		service: updatesvc.New(
			releases,
			entity.Build{Version: version, OS: "darwin", Arch: "arm64"},
			config.Update{
				Check:    check,
				Interval: time.Hour,
				Timeout:  time.Second,
				Feed:     "https://releases.example/latest",
			},
		),
	}
}

func (h *harness) publishes(version string) {
	h.releases.EXPECT().
		Latest(gomock.Any()).
		Return(entity.Release{
			Version: version,
			URL:     "https://github.com/usenorn/runner/releases/tag/" + version,
		}, nil)
}
