package control_test

import (
	"context"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
)

func TestACallOnARunThatCarriesNothingIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	running(t, h, "exec-01BARE")

	_, err := h.client.Services(ctx, "exec-01BARE")
	if err == nil {
		t.Fatalf(
			"a caller that proved nothing was served a run's services. Anything on this machine " +
				"could then drive somebody else's run by naming it",
		)
	}

	if !strings.Contains(err.Error(), entity.ExecutionTokenVariable) {
		t.Fatalf(
			"the refusal was %q, which never names %s. Whoever hits this has to be told what "+
				"they are missing, or they will assume the daemon is broken",
			err, entity.ExecutionTokenVariable,
		)
	}
}

func TestARunCannotActOnAnotherRunWithItsOwnToken(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	running(t, h, "exec-01MINE")
	running(t, h, "exec-01YOURS")

	mine := h.as(t, "exec-01MINE")

	if _, err := mine.Services(ctx, "exec-01YOURS"); err == nil {
		t.Fatalf(
			"one run listed another run's services. A tool call naming a service or a port of " +
				"another execution has to be refused, and this is where that is decided",
		)
	}

	if _, err := mine.StartService(ctx, "exec-01YOURS", control.ServiceRequest{
		Name:    "api",
		Command: []string{"sleep", "30"},
	}); err == nil {
		t.Fatalf("one run started a process inside another run's workspace")
	}

	if _, err := mine.AllocatePort(ctx, "exec-01YOURS", "web"); err == nil {
		t.Fatalf("one run took a port out of another run's allocation")
	}
}

func TestATokenIsRefusedOnceItsRunIsOver(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	running(t, h, "exec-01SPENT")

	spent := h.as(t, "exec-01SPENT")

	if _, err := spent.Services(ctx, "exec-01SPENT"); err != nil {
		t.Fatalf("a run could not read its own services while it was live: %v", err)
	}

	h.tokens.Release(ctx, "exec-01SPENT")

	if _, err := spent.Services(ctx, "exec-01SPENT"); err == nil {
		t.Fatalf(
			"a token still worked after its run was torn down. Anything that kept a copy of it " +
				"would keep the authority of a run nobody is watching",
		)
	}
}

func TestTheOperatorsOwnCommandsNeedNoRunToken(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()

	running(t, h, "exec-01WATCHED")

	if _, err := h.client.Status(ctx); err != nil {
		t.Fatalf("a person could not read this machine's status without a run's token: %v", err)
	}

	if _, err := h.client.Executions(ctx); err != nil {
		t.Fatalf("a person could not list what this machine is holding: %v", err)
	}

	if _, err := h.client.Logs(ctx, "exec-01WATCHED"); err != nil &&
		strings.Contains(err.Error(), entity.ExecutionTokenVariable) {
		t.Fatalf("a person was asked for a run's own token to read its timeline: %v", err)
	}
}
