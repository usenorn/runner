package supervisor_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestReadingAServicesOutputCanAskForOnlyTheLinesThatMatter(t *testing.T) {
	h := newHarness(t, 45900, 45999)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()

	run := h.prepared(t, "exec-01GREP")

	_, err := h.service.Start(ctx, run.ID, entity.Service{
		Name: "api",
		Command: []string{"sh", "-c",
			"echo listening on 1; echo GET / 200; echo ERROR the migration failed; " +
				"echo GET /x 200; sleep 300",
		},
		Health: entity.Health{Kind: entity.HealthLog, Pattern: "listening on"},
	})
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	defer func() { _ = h.service.Release(ctx, run.ID) }()

	h.awaitState(t, run.ID, "api", entity.ServiceHealthy)

	h.await(t, "waited for the service to have said something worth filtering", func() bool {
		lines, err := h.service.Logs(ctx, run.ID, "api", entity.LogQuery{})

		return err == nil && slices.ContainsFunc(lines, func(line string) bool {
			return strings.Contains(line, "ERROR")
		})
	})

	matched, err := h.service.Logs(ctx, run.ID, "api", entity.LogQuery{Grep: "^ERROR"})
	if err != nil {
		t.Fatalf("read only the lines that matter: %v", err)
	}

	if len(matched) != 1 || !strings.Contains(matched[0], "the migration failed") {
		t.Fatalf(
			"asking for the errors answered %v. An agent that has to read a whole log to find "+
				"one line spends its turn on the reading",
			matched,
		)
	}

	if _, err := h.service.Logs(
		ctx, run.ID, "api", entity.LogQuery{Grep: "("},
	); !errors.Is(err, entity.ErrServiceInvalid) {
		t.Fatalf("a pattern that is not one was quietly ignored: %v", err)
	}
}

func TestAPortCanBeReservedByNameWithoutStartingAnythingOnIt(t *testing.T) {
	h := newHarness(t, 45800, 45899)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()

	run := h.prepared(t, "exec-01PORT")

	defer func() { _ = h.service.Release(ctx, run.ID) }()

	port, err := h.service.Port(ctx, run.ID, "web")
	if err != nil {
		t.Fatalf("reserve a port by name: %v", err)
	}

	if port < 45800 || port > 45899 {
		t.Fatalf("a run was handed port %d, outside the range this machine was given", port)
	}

	again, err := h.service.Port(ctx, run.ID, "web")
	if err != nil {
		t.Fatalf("reserve the same name again: %v", err)
	}

	if again != port {
		t.Fatalf(
			"asking twice for the same name gave %d and then %d, so whatever was told the "+
				"first one is now pointed somewhere nothing is listening",
			port, again,
		)
	}

	if _, err := h.service.Port(ctx, run.ID, "Not A Name"); !errors.Is(
		err, entity.ErrServiceInvalid,
	) {
		t.Fatalf("a name nothing can be reserved under was accepted: %v", err)
	}
}
