package port_test

import (
	"context"
	"errors"
	"testing"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	portrepo "github.com/usenorn/runner/internal/repository/port"
)

func TestAskingTwiceForTheSameServicesPortAnswersTheSamePortTwice(t *testing.T) {
	ports := portrepo.New(config.Runner{PortRange: [2]int{45300, 45399}})
	ctx := context.Background()

	first, err := ports.Reserve(ctx, "exec-01ABC", "api")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}

	again, err := ports.Reserve(ctx, "exec-01ABC", "api")
	if err != nil {
		t.Fatalf("reserve the same port again: %v", err)
	}

	if first != again {
		t.Fatalf("the same service was given %d and then %d", first, again)
	}

	if first < 45300 || first > 45399 {
		t.Fatalf("%d is outside the range this machine was given", first)
	}
}

func TestTwoRunsOfTheSameProjectNeverGetTheSamePort(t *testing.T) {
	ports := portrepo.New(config.Runner{PortRange: [2]int{45400, 45499}})
	ctx := context.Background()

	first, err := ports.Reserve(ctx, "exec-01ABC", "web")
	if err != nil {
		t.Fatalf("reserve a port for the first run: %v", err)
	}

	second, err := ports.Reserve(ctx, "exec-01XYZ", "web")
	if err != nil {
		t.Fatalf("reserve a port for the second run: %v", err)
	}

	if first == second {
		t.Fatalf(
			"both runs were given port %d, so whichever starts second cannot bind it", first,
		)
	}
}

func TestARangeWithNothingLeftInItSaysSoRatherThanHandingOutAPortTwice(t *testing.T) {
	ports := portrepo.New(config.Runner{PortRange: [2]int{45500, 45501}})
	ctx := context.Background()

	for _, name := range []string{"api", "web"} {
		if _, err := ports.Reserve(ctx, "exec-01ABC", name); err != nil {
			t.Fatalf("reserve a port for %s: %v", name, err)
		}
	}

	if _, err := ports.Reserve(ctx, "exec-01ABC", "worker"); !errors.Is(
		err, entity.ErrPortsExhausted,
	) {
		t.Fatalf("a third service in a range of two answered %v", err)
	}
}

func TestGivingARunBackFreesEveryPortItHeldAndLeavesTheOthersAlone(t *testing.T) {
	ports := portrepo.New(config.Runner{PortRange: [2]int{45600, 45699}})
	ctx := context.Background()

	for _, name := range []string{"api", "web"} {
		if _, err := ports.Reserve(ctx, "exec-01ABC", name); err != nil {
			t.Fatalf("reserve a port for %s: %v", name, err)
		}
	}

	kept, err := ports.Reserve(ctx, "exec-01XYZ", "api")
	if err != nil {
		t.Fatalf("reserve a port for the other run: %v", err)
	}

	ports.Release(ctx, "exec-01ABC")

	held, err := ports.Held(ctx, "exec-01ABC")
	if err != nil {
		t.Fatalf("read what the run still holds: %v", err)
	}

	if len(held) != 0 {
		t.Fatalf("a run that was given back still holds %v", held)
	}

	other, err := ports.Held(ctx, "exec-01XYZ")
	if err != nil {
		t.Fatalf("read what the other run holds: %v", err)
	}

	if other["api"] != kept {
		t.Fatalf("giving one run back moved another run's port to %d", other["api"])
	}
}
