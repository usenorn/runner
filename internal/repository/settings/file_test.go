package settings_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	settingsrepo "github.com/usenorn/runner/internal/repository/settings"
)

func write(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestAFolderWithNoSettingsAsksForNothing(t *testing.T) {
	held, err := settingsrepo.New().Load(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("read the settings of an untouched folder: %v", err)
	}

	if held != (repository.CodebaseSettings{}) {
		t.Fatalf("an untouched folder asked for %+v", held)
	}
}

func TestAFolderCanAskForItsUncommittedWorkToTravel(t *testing.T) {
	root := t.TempDir()

	write(t, filepath.Join(root, entity.SettingsDir, entity.SettingsFile), `version: 1
snapshot:
  local_changes: include
  base: head
  fetch: false
env:
  pass_files:
    - backend/.env
`)

	held, err := settingsrepo.New().Load(context.Background(), root)
	if err != nil {
		t.Fatalf("read the settings of %s: %v", root, err)
	}

	if held.LocalChanges != "include" || held.Base != "head" {
		t.Fatalf("the folder asked for %+v", held)
	}

	if held.Fetch == nil || *held.Fetch {
		t.Fatalf("the folder asked not to fetch and that was not read back")
	}

	if held.GitMode != "" {
		t.Fatalf(
			"git_mode came back as %q from a file that never mentions it; a setting a folder did "+
				"not write must not override the runner's own",
			held.GitMode,
		)
	}
}

func TestANornignoreIsReadWhereItIsAndNowhereElse(t *testing.T) {
	root := t.TempDir()
	settings := settingsrepo.New()

	rules, err := settings.Ignores(context.Background(), root)
	if err != nil || rules != nil {
		t.Fatalf("a folder with no %s came back with %v, %v", entity.IgnoreFileName, rules, err)
	}

	write(t, filepath.Join(root, entity.IgnoreFileName), "# notes\nscratch/\n!keep.txt\n")

	rules, err = settings.Ignores(context.Background(), root)
	if err != nil {
		t.Fatalf("read %s: %v", entity.IgnoreFileName, err)
	}

	if len(rules) != 2 {
		t.Fatalf("%d rules were read from a file holding a comment and two rules", len(rules))
	}
}

const specPlan = `version: 1
services:
  postgres:
    kind: compose
    compose_service: postgres
    health: { tcp: "${ports.postgres}" }
  api:
    kind: process
    cwd: backend
    command: [go, run, ./cmd/api]
    environment: { PORT: "${ports.api}" }
    requires: [postgres]
    health: { http: { path: /health, port: "${ports.api}" } }
  web:
    kind: process
    cwd: frontend
    command: [pnpm, dev, --port, "${ports.web}"]
    environment: { API_URL: "http://127.0.0.1:${ports.api}" }
    requires: [api]
    health: { http: { path: /, port: "${ports.web}" } }
previews:
  - name: Application
    service: web
  - name: API
    service: api
    path: /api
`

func TestTheRunPlanDeclaresEveryServiceAndPreviewAsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), entity.SettingsDir, entity.PlanFile)
	write(t, path, specPlan)

	plan, err := settingsrepo.New().Definition(context.Background(), path)
	if err != nil {
		t.Fatalf("read the run plan: %v", err)
	}

	if len(plan.Services) != 3 || len(plan.Previews) != 2 {
		t.Fatalf("read %d services and %d previews, want 3 and 2", len(plan.Services), len(plan.Previews))
	}

	api, _ := plan.Service("api")

	if api.Kind != entity.PlanServiceProcess || api.Service.Dir != "backend" ||
		api.Service.Health.Kind != entity.HealthHTTP || api.Service.Health.Path != "/health" {
		t.Fatalf("the api service was read as %+v", api)
	}

	if api.Service.Environment["PORT"] != "${ports.api}" {
		t.Fatalf("the api environment was read as %v; variable names must keep their case", api.Service.Environment)
	}

	postgres, _ := plan.Service("postgres")

	if postgres.Kind != entity.PlanServiceCompose || postgres.Service.Health.Kind != entity.HealthTCP {
		t.Fatalf("postgres was read as %+v", postgres)
	}

	if plan.Previews[1] != (entity.PlanPreview{Name: "API", Service: "api", Path: "/api"}) {
		t.Fatalf("the second preview was read as %+v", plan.Previews[1])
	}
}

func TestARunPlanThatIsNotYAMLIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), entity.PlanFile)
	write(t, path, "services: [unterminated")

	if _, err := settingsrepo.New().Definition(context.Background(), path); !errors.Is(err, entity.ErrPlanInvalid) {
		t.Fatalf("a broken run plan answered %v", err)
	}
}
