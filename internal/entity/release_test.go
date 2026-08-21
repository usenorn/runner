package entity_test

import (
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestAReleaseIsOnlyNewerWhenBothSidesAreRealVersions(t *testing.T) {
	cases := []struct {
		name    string
		build   string
		release string
		newer   bool
	}{
		{"a later patch", "v0.1.0", "v0.1.1", true},
		{"a later minor", "v0.1.9", "v0.2.0", true},
		{"the same version", "v1.2.3", "v1.2.3", false},
		{"an older release", "v1.2.3", "v1.2.2", false},
		{"a prerelease of the version already installed", "v1.0.0", "v1.0.0-rc.1", false},
		{"the release a prerelease was leading up to", "v1.0.0-rc.1", "v1.0.0", true},
		{"a later prerelease", "v1.0.0-rc.1", "v1.0.0-rc.2", true},
		{"a development build", entity.DevelopmentVersion, "v9.9.9", false},
		{"an edge build", "edge-1a2b3c4", "v9.9.9", false},
		{"a feed that answered with nonsense", "v1.0.0", "latest", false},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			release := entity.Release{Version: test.release}
			build := entity.Build{Version: test.build}

			if got := release.NewerThan(build); got != test.newer {
				t.Fatalf(
					"a runner on %s told that %s exists reported newer=%t, want %t",
					test.build, test.release, got, test.newer,
				)
			}
		})
	}
}

func TestADevelopmentBuildIsNeverTreatedAsAReleasedOne(t *testing.T) {
	for _, version := range []string{entity.DevelopmentVersion, "", "edge-1a2b3c4", "1.0.0"} {
		if (entity.Build{Version: version}).Released() {
			t.Errorf("a build stamped %q claims to be a release, so it would nag about updates", version)
		}
	}

	if !(entity.Build{Version: "v1.0.0"}).Released() {
		t.Fatalf("a tagged build does not count as a release, so it would never check for updates")
	}
}
