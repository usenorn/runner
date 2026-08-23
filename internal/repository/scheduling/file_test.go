package scheduling_test

import (
	"context"
	"os"
	"testing"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/pkg/statedir"
	schedulingrepo "github.com/usenorn/runner/internal/repository/scheduling"
)

func store(t *testing.T) (*statedir.Dir, context.Context) {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("create the state directory: %v", err)
	}

	return dir, context.Background()
}

func TestAMachineSomebodyPausedIsStillPausedAfterItStops(t *testing.T) {
	dir, ctx := store(t)

	if err := schedulingrepo.New(dir).Pause(ctx, true); err != nil {
		t.Fatalf("pause the machine: %v", err)
	}

	paused, err := schedulingrepo.New(dir).Paused(ctx)
	if err != nil {
		t.Fatalf("read back whether it is paused: %v", err)
	}

	if !paused {
		t.Fatalf(
			"a machine that was paused came back taking work, so somebody who stopped it would " +
				"find it running jobs anyway",
		)
	}
}

func TestResumingIsRememberedTheSameWayPausingIs(t *testing.T) {
	dir, ctx := store(t)
	scheduling := schedulingrepo.New(dir)

	if err := scheduling.Pause(ctx, true); err != nil {
		t.Fatalf("pause the machine: %v", err)
	}

	if err := scheduling.Pause(ctx, false); err != nil {
		t.Fatalf("resume the machine: %v", err)
	}

	paused, err := schedulingrepo.New(dir).Paused(ctx)
	if err != nil {
		t.Fatalf("read back whether it is paused: %v", err)
	}

	if paused {
		t.Fatalf("a machine somebody resumed came back paused and would turn every offer down")
	}
}

func TestAMachineNobodyEverPausedTakesWork(t *testing.T) {
	dir, ctx := store(t)

	paused, err := schedulingrepo.New(dir).Paused(ctx)
	if err != nil {
		t.Fatalf("a machine with no scheduling file could not be asked whether it is paused: %v", err)
	}

	if paused {
		t.Fatalf("a machine nobody paused refused work")
	}
}

func TestOneUnreadableFlagDoesNotStopAMachineTakingWork(t *testing.T) {
	dir, ctx := store(t)

	if err := os.WriteFile(dir.Scheduling(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write a broken scheduling file: %v", err)
	}

	paused, err := schedulingrepo.New(dir).Paused(ctx)
	if err != nil {
		t.Fatalf(
			"a broken scheduling file stopped the machine reading its own state: %v", err,
		)
	}

	if paused {
		t.Fatalf(
			"a machine sat idle because one file it could not read might have said it was paused",
		)
	}
}
