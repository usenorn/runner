package entity_test

import (
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func process(name string, requires ...string) entity.PlanService {
	return entity.PlanService{
		Kind: entity.PlanServiceProcess,
		Service: entity.Service{
			Name:     name,
			Command:  []string{"run", name},
			Requires: requires,
		},
	}
}

func names(services []entity.Service) string {
	named := make([]string, 0, len(services))

	for _, service := range services {
		named = append(named, service.Name)
	}

	return strings.Join(named, ",")
}

func TestServicesStartAfterEverythingTheyNeed(t *testing.T) {
	plan := entity.PlanDefinition{Services: []entity.PlanService{
		process("web", "api"), process("api", "cache"), process("cache"),
	}}

	if got := names(plan.Runnable()); got != "cache,api,web" {
		t.Fatalf("the services start as %s, want cache,api,web", got)
	}
}

func TestAServiceThatNeedsAComposeServiceCannotRunHere(t *testing.T) {
	plan := entity.PlanDefinition{Services: []entity.PlanService{
		{Kind: entity.PlanServiceCompose, Service: entity.Service{Name: "postgres"}},
		process("api", "postgres"),
		process("web"),
	}}

	if got := names(plan.Runnable()); got != "web" {
		t.Fatalf("runnable services are %s, want only web", got)
	}

	reason, unrunnable := plan.Unrunnable("postgres")
	if !unrunnable || !strings.Contains(reason, "compose") {
		t.Fatalf("postgres answered %q, %t", reason, unrunnable)
	}

	if reason, unrunnable := plan.Unrunnable("api"); !unrunnable || !strings.Contains(reason, "needs") {
		t.Fatalf("api answered %q, %t", reason, unrunnable)
	}

	if _, unrunnable := plan.Unrunnable("web"); unrunnable {
		t.Fatal("web can run, and was said not to")
	}
}

func TestServicesThatNeedEachOtherNeverStart(t *testing.T) {
	plan := entity.PlanDefinition{Services: []entity.PlanService{process("a", "b"), process("b", "a")}}

	if got := names(plan.Runnable()); got != "" {
		t.Fatalf("a cycle started %s", got)
	}
}

func TestAPreviewUndeclaredInThePlanIsSaidSo(t *testing.T) {
	if reason, unrunnable := (entity.PlanDefinition{}).Unrunnable("docs"); !unrunnable ||
		!strings.Contains(reason, "declares no service named docs") {
		t.Fatalf("an undeclared service answered %q", reason)
	}
}

func TestAPreviewIsExposedUnderANameThisMachineAccepts(t *testing.T) {
	for name, want := range map[string]string{
		"Application":    "application",
		"Admin console":  "admin-console",
		" API (v2) ":     "api-v2",
		"already-fine_1": "already-fine_1",
	} {
		if got := (entity.PlanPreview{Name: name}).Slug(); got != want {
			t.Errorf("%q became %q, want %q", name, got, want)
		}
	}
}
