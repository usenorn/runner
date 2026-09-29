package run_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/entity"
	runrepo "github.com/usenorn/runner/internal/repository/run"
)

func TestThePlanIsTheLatestOneTheAgentWrote(t *testing.T) {
	dir, ctx := store(t)
	runs := runrepo.New(dir)

	if _, err := runs.Open(ctx, "exec-01PLN"); err != nil {
		t.Fatalf("open a run: %v", err)
	}

	if _, err := runs.LatestPlan(ctx, "exec-01PLN"); !errors.Is(err, entity.ErrPlanMissing) {
		t.Fatalf("a run that wrote no plan reads as %v, want the plan missing", err)
	}

	plans := entity.RunHomeOf(dir.Run("exec-01PLN")).Plans()
	if err := os.MkdirAll(plans, 0o700); err != nil {
		t.Fatalf("make the plans directory: %v", err)
	}

	earlier := time.Now().Add(-time.Minute)

	for name, body := range map[string]string{"first.md": "Drop the table.", "notes.txt": "scratch"} {
		path := filepath.Join(plans, name)

		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}

		if err := os.Chtimes(path, earlier, earlier); err != nil {
			t.Fatalf("age %s: %v", name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(plans, "second.md"), []byte("  Archive the table.\n"), 0o600); err != nil {
		t.Fatalf("write the revision: %v", err)
	}

	body, err := runs.LatestPlan(ctx, "exec-01PLN")
	if err != nil || body != "Archive the table." {
		t.Fatalf("the plan reads as %q (%v), want the revision the agent wrote last", body, err)
	}
}
