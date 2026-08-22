package servicelog_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	servicelogrepo "github.com/usenorn/runner/internal/repository/servicelog"
)

func store(t *testing.T) (*statedir.Dir, context.Context) {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("create the state directory: %v", err)
	}

	return dir, context.Background()
}

func TestWhatAServiceWroteIsStillThereAfterItHasGone(t *testing.T) {
	dir, ctx := store(t)
	logs := servicelogrepo.New(dir)

	sink, err := logs.Open(ctx, "exec-01ABC", "api")
	if err != nil {
		t.Fatalf("open a service log: %v", err)
	}

	if _, err := sink.Write([]byte("listening on 43001\nserving\n")); err != nil {
		t.Fatalf("write to a service log: %v", err)
	}

	if err := sink.Close(); err != nil {
		t.Fatalf("close a service log: %v", err)
	}

	path := filepath.Join(
		dir.Run("exec-01ABC"), entity.RunLogsDir, entity.RunServiceLogsDir, "api.log",
	)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a service log is not where a person would look for it: %v", err)
	}

	lines, err := logs.Tail(ctx, "exec-01ABC", "api", 0)
	if err != nil {
		t.Fatalf("read a service log: %v", err)
	}

	if len(lines) != 2 || lines[0] != "listening on 43001" || lines[1] != "serving" {
		t.Fatalf("the service log came back as %q", lines)
	}
}

func TestAskingForTheLastFewLinesAnswersTheLastFewRatherThanTheFirst(t *testing.T) {
	dir, ctx := store(t)
	logs := servicelogrepo.New(dir)

	sink, err := logs.Open(ctx, "exec-01ABC", "api")
	if err != nil {
		t.Fatalf("open a service log: %v", err)
	}

	for _, line := range []string{"one\n", "two\n", "three\n"} {
		if _, err := sink.Write([]byte(line)); err != nil {
			t.Fatalf("write to a service log: %v", err)
		}
	}

	_ = sink.Close()

	lines, err := logs.Tail(ctx, "exec-01ABC", "api", 2)
	if err != nil {
		t.Fatalf("read a service log: %v", err)
	}

	if len(lines) != 2 || lines[0] != "two" || lines[1] != "three" {
		t.Fatalf("the last two lines came back as %q", lines)
	}
}

func TestAServiceThatNeverRanSaysSoRatherThanAnsweringNothing(t *testing.T) {
	dir, ctx := store(t)

	if _, err := servicelogrepo.New(dir).Tail(ctx, "exec-01ABC", "ghost", 0); !errors.Is(
		err, entity.ErrServiceUnknown,
	) {
		t.Fatalf("reading a log nothing wrote answered %v", err)
	}
}
