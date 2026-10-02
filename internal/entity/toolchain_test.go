package entity_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func toolNames(required []entity.Requirement) []string {
	named := make([]string, 0, len(required))
	for _, requirement := range required {
		named = append(named, requirement.Tool.Name)
	}

	return named
}

func TestEachManifestNamesTheToolsItsRepositoryBuildsWith(t *testing.T) {
	cases := []struct {
		name     string
		manifest entity.Manifest
		want     []string
	}{
		{name: "a go module", manifest: entity.Manifest{Files: []string{"go.mod", "Makefile"}}, want: []string{"go", "make"}},
		{name: "a bun lockfile", manifest: entity.Manifest{Files: []string{"package.json", "bun.lock"}}, want: []string{"bun"}},
		{
			name:     "packageManager wins over a stray lockfile",
			manifest: entity.Manifest{Files: []string{"package.json", "package-lock.json"}, PackageManager: "pnpm@10.4.1"},
			want:     []string{"node", "pnpm"},
		},
		{name: "plain package.json", manifest: entity.Manifest{Files: []string{"package.json"}}, want: []string{"node", "npm"}},
		{name: "uv over bare python", manifest: entity.Manifest{Files: []string{"pyproject.toml", "uv.lock"}}, want: []string{"uv"}},
		{name: "nothing to build", manifest: entity.Manifest{Files: nil}, want: []string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toolNames(entity.Requirements([]entity.Manifest{tc.manifest}))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("required %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAToolSharedByTwoRepositoriesIsRequiredOnceNamingBoth(t *testing.T) {
	required := entity.Requirements([]entity.Manifest{
		{RelPath: "platform", Files: []string{"go.mod"}},
		{RelPath: "genesis", Files: []string{"go.mod"}},
	})

	if len(required) != 1 || !slices.Equal(required[0].Needed, []string{"platform", "genesis"}) {
		t.Fatalf("required %+v", required)
	}
}

func TestAnUnusableToolchainIsReportedInFullInOneMessage(t *testing.T) {
	report := entity.ToolchainReport{
		{Requirement: entity.Requirements([]entity.Manifest{{RelPath: "platform", Files: []string{"go.mod"}}})[0],
			State: entity.ToolSnap, Path: "/snap/bin/go"},
		{Requirement: entity.Requirements([]entity.Manifest{{RelPath: "front", Files: []string{"package.json", "bun.lock"}}})[0],
			State: entity.ToolMissing},
		{Requirement: entity.Requirements([]entity.Manifest{{RelPath: "tools", Files: []string{"Makefile"}}})[0],
			State: entity.ToolReady, Version: "GNU Make 4.4"},
	}

	err := report.Problem()
	if !errors.Is(err, entity.ErrToolchainUnusable) {
		t.Fatalf("an unusable toolchain answered %v", err)
	}

	for _, wanted := range []string{"go is installed as a snap", "go.dev", "bun is not installed", "bun.sh", "norn runner doctor"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Fatalf("the failure never says %q, so the next missing tool is found a run later:\n%s", wanted, err)
		}
	}

	if strings.Contains(err.Error(), "GNU Make") {
		t.Fatalf("a tool that works is listed among the broken ones:\n%s", err)
	}
}
