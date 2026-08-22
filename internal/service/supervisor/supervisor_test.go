package supervisor_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/usenorn/runner/internal/entity"
)

func TestTwoRunsOfTheSameProjectGetTheirOwnPortsAndNeitherSeesTheOthersServices(t *testing.T) {
	h := newHarness(t, 45700, 45799)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()

	first := h.prepared(t, "exec-01FIRST")
	second := h.prepared(t, "exec-01SECOND")

	wanted := entity.Service{
		Name:    "api",
		Command: []string{"sh", "-c", "echo listening on $NORN_PORT_API; sleep 300"},
		Health:  entity.Health{Kind: entity.HealthLog, Pattern: "listening on"},
	}

	one, err := h.service.Start(ctx, first.ID, wanted)
	if err != nil {
		t.Fatalf("start the first run's api: %v", err)
	}

	two, err := h.service.Start(ctx, second.ID, wanted)
	if err != nil {
		t.Fatalf("start the second run's api: %v", err)
	}

	defer func() {
		_ = h.service.Release(ctx, first.ID)
		_ = h.service.Release(ctx, second.ID)
	}()

	if one.Port == 0 || two.Port == 0 || one.Port == two.Port {
		t.Fatalf(
			"the two runs were given ports %d and %d, so whichever starts second cannot bind",
			one.Port, two.Port,
		)
	}

	h.awaitState(t, first.ID, "api", entity.ServiceHealthy)
	h.awaitState(t, second.ID, "api", entity.ServiceHealthy)

	for _, execution := range []string{first.ID, second.ID} {
		records := h.list(t, execution)

		if len(records) != 1 || records[0].Name != "api" {
			t.Fatalf("%s can see %+v, which is not only its own services", execution, records)
		}
	}

	if !strings.Contains(h.wrote(t, first.ID, "api"), "listening on") {
		t.Fatalf("the first run's api wrote nothing to its own log")
	}

	stored := h.stored(t, first.ID)

	if stored.Ports["api"] != one.Port || len(stored.Services) != 1 {
		t.Fatalf(
			"what the run wrote down does not name its port and process: %+v", stored,
		)
	}

	if stored.Services[0].PID == 0 {
		t.Fatalf("the run wrote down no process id, so nothing could clean up after a restart")
	}
}

func TestAServiceThatKeepsStoppingIsTriedAgainAndThenReportedUnhealthy(t *testing.T) {
	h := newHarness(t, 45800, 45899)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()
	execution := h.prepared(t, "exec-01CRASH")

	if _, err := h.service.Start(ctx, execution.ID, held("api", "exit 9")); err != nil {
		t.Fatalf("start a service that will not stay up: %v", err)
	}

	record := h.awaitState(t, execution.ID, "api", entity.ServiceUnhealthy)

	if record.Attempts != settings().RestartAttempts+1 {
		t.Fatalf("the service was tried %d times before it was given up on", record.Attempts)
	}

	if !strings.Contains(record.Reason, "exit code 9") {
		t.Fatalf("the reason it was given up on does not name the code: %q", record.Reason)
	}

	said := h.awaitSaid(t, execution.ID, "starting it again")

	if !strings.Contains(said, "attempt 1 of 3") {
		t.Fatalf("the timeline does not say which attempt this was: %q", said)
	}

	if record.PID != 0 {
		t.Fatalf("a service that has stopped for good still names a process %d", record.PID)
	}

	for _, entry := range h.timeline(t, execution.ID) {
		if strings.Contains(entry.Reason, "api is healthy") {
			t.Fatalf(
				"a service that never stayed up was called healthy: %q; the timeline has to be "+
					"readable as what happened",
				entry.Reason,
			)
		}
	}

	h.awaitReported(t, execution.ID, "api is unhealthy")
}

func TestGivingARunBackLeavesNothingItStartedRunning(t *testing.T) {
	h := newHarness(t, 45900, 45999)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()
	execution := h.prepared(t, "exec-01TEARDOWN")

	record, err := h.service.Start(
		ctx, execution.ID, held("api", "sleep 300 & echo up; sleep 300"),
	)
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	group := record.PID

	h.awaitState(t, execution.ID, "api", entity.ServiceHealthy)

	if err := h.service.Release(ctx, execution.ID); err != nil {
		t.Fatalf("give the run back: %v", err)
	}

	h.await(t, "waited for everything the run started to be gone", func() bool {
		return syscall.Kill(-group, 0) == syscall.ESRCH
	})

	ports, err := h.ports.Held(ctx, execution.ID)
	if err != nil {
		t.Fatalf("read what the run still holds: %v", err)
	}

	if len(ports) != 0 {
		t.Fatalf("a run that was given back still holds %v", ports)
	}

	if !strings.Contains(h.wrote(t, execution.ID, "api"), "up") {
		t.Fatalf("giving the run back took the service's log with it")
	}
}

func TestShuttingTheMachineDownStopsEveryServiceItWasRunning(t *testing.T) {
	h := newHarness(t, 46000, 46099)
	stop := h.start(t)

	ctx := context.Background()
	execution := h.prepared(t, "exec-01DRAIN")

	record, err := h.service.Start(ctx, execution.ID, held("api", "sleep 300"))
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	h.awaitState(t, execution.ID, "api", entity.ServiceHealthy)

	stop()

	if syscall.Kill(-record.PID, 0) != syscall.ESRCH {
		t.Fatalf(
			"a service outlived the machine that started it, so shutting down leaves work behind",
		)
	}
}

func TestAServiceWaitsForWhatItNeedsRatherThanStartingIntoNothing(t *testing.T) {
	h := newHarness(t, 46100, 46199)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()
	execution := h.prepared(t, "exec-01ORDER")

	defer func() { _ = h.service.Release(ctx, execution.ID) }()

	web := entity.Service{
		Name:     "web",
		Command:  []string{"sh", "-c", "sleep 300"},
		Requires: []string{"api"},
	}

	_, err := h.service.Start(ctx, execution.ID, web)

	if !errors.Is(err, entity.ErrServiceWaiting) {
		t.Fatalf("a service needing one that is not running answered %v", err)
	}

	if !strings.Contains(err.Error(), "api") {
		t.Fatalf("the refusal does not name what is missing: %v", err)
	}

	if _, err := h.service.Start(ctx, execution.ID, held("api", "sleep 300")); err != nil {
		t.Fatalf("start what the other one needs: %v", err)
	}

	h.awaitState(t, execution.ID, "api", entity.ServiceHealthy)

	if _, err := h.service.Start(ctx, execution.ID, web); err != nil {
		t.Fatalf("start a service once what it needs is healthy: %v", err)
	}
}

func TestOneServiceReachesAnotherByTheNameItWasGivenRatherThanAGuessedPort(t *testing.T) {
	h := newHarness(t, 46200, 46299)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()
	execution := h.prepared(t, "exec-01PORTS")

	defer func() { _ = h.service.Release(ctx, execution.ID) }()

	api, err := h.service.Start(ctx, execution.ID, entity.Service{
		Name:    "api",
		Command: []string{"sh", "-c", "sleep 300"},
		Health:  entity.Health{Kind: entity.HealthTCP},
	})
	if err != nil {
		t.Fatalf("start the api: %v", err)
	}

	if api.Port == 0 {
		t.Fatalf("a service that is checked on its port was given none")
	}

	web, err := h.service.Start(ctx, execution.ID, entity.Service{
		Name:        "web",
		Command:     []string{"sh", "-c", "echo api is at $API_URL, mine is $NORN_PORT_API; sleep 300"},
		Environment: map[string]string{"API_URL": "http://127.0.0.1:${ports.api}"},
		Health: entity.Health{
			Kind:    entity.HealthLog,
			Pattern: "api is at http://127.0.0.1:" + itoa(api.Port),
		},
	})
	if err != nil {
		t.Fatalf("start the web: %v", err)
	}

	h.awaitState(t, execution.ID, "web", entity.ServiceHealthy)

	if !strings.Contains(h.wrote(t, execution.ID, "web"), "mine is "+itoa(api.Port)) {
		t.Fatalf(
			"the api's port did not reach the web as an environment variable: %q",
			h.wrote(t, execution.ID, "web"),
		)
	}

	if web.Port == api.Port {
		t.Fatalf("both services were given port %d", web.Port)
	}
}

func TestAStepNamingAPortNoServiceReservedIsRefusedByName(t *testing.T) {
	h := newHarness(t, 46300, 46399)
	stop := h.start(t)

	defer stop()

	execution := h.prepared(t, "exec-01MISSING")

	_, err := h.service.Step(context.Background(), execution.ID, entity.Step{
		Name:    "seed",
		Command: []string{"sh", "-c", "echo ${ports.database}"},
	})

	if !errors.Is(err, entity.ErrPortUnknown) {
		t.Fatalf("a step naming a port no service reserved answered %v", err)
	}

	if !strings.Contains(err.Error(), "database") {
		t.Fatalf("the refusal does not name the port that is missing: %v", err)
	}
}

func TestAServiceThatNeverComesUpIsReportedUnhealthyWithWhatWasTried(t *testing.T) {
	h := newHarness(t, 46400, 46499)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()
	execution := h.prepared(t, "exec-01SILENT")

	started, err := h.service.Start(ctx, execution.ID, entity.Service{
		Name:    "api",
		Command: []string{"sh", "-c", "sleep 300"},
		Health:  entity.Health{Kind: entity.HealthLog, Pattern: "never written"},
	})
	if err != nil {
		t.Fatalf("start a service that says nothing: %v", err)
	}

	record := h.awaitState(t, execution.ID, "api", entity.ServiceUnhealthy)

	if !strings.Contains(record.Reason, "did not come up within") {
		t.Fatalf(
			"the reason it was called unhealthy does not say what was waited for: %q",
			record.Reason,
		)
	}

	if err := h.service.Release(ctx, execution.ID); err != nil {
		t.Fatalf("give the run back: %v", err)
	}

	h.await(t, "waited for a service that never came up to be stopped anyway", func() bool {
		return syscall.Kill(-started.PID, 0) == syscall.ESRCH
	})
}

func TestStartingAServiceThatIsAlreadyRunningChangesNothing(t *testing.T) {
	h := newHarness(t, 46500, 46599)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()
	execution := h.prepared(t, "exec-01AGAIN")

	defer func() { _ = h.service.Release(ctx, execution.ID) }()

	first, err := h.service.Start(ctx, execution.ID, held("api", "sleep 300"))
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	again, err := h.service.Start(ctx, execution.ID, held("api", "sleep 300"))
	if err != nil {
		t.Fatalf("start the same service again: %v", err)
	}

	if again.PID != first.PID {
		t.Fatalf(
			"starting the same service twice left two processes, %d and %d", first.PID, again.PID,
		)
	}
}

func TestRestartingAServiceKeepsThePortAnythingWasToldToUse(t *testing.T) {
	h := newHarness(t, 46600, 46699)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()
	execution := h.prepared(t, "exec-01RESTART")

	defer func() { _ = h.service.Release(ctx, execution.ID) }()

	first, err := h.service.Start(ctx, execution.ID, entity.Service{
		Name:    "api",
		Command: []string{"sh", "-c", "echo up; sleep 300"},
		Health:  entity.Health{Kind: entity.HealthLog, Pattern: "up"},
	})
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	h.awaitState(t, execution.ID, "api", entity.ServiceHealthy)

	again, err := h.service.Restart(ctx, execution.ID, "api")
	if err != nil {
		t.Fatalf("restart a service: %v", err)
	}

	if again.Port != first.Port {
		t.Fatalf(
			"restarting moved the service from port %d to %d, so anything holding the first is "+
				"pointed at nothing",
			first.Port, again.Port,
		)
	}

	if again.PID == first.PID {
		t.Fatalf("restarting left the same process %d running", first.PID)
	}

	h.await(t, "waited for the first process to be gone", func() bool {
		return syscall.Kill(-first.PID, 0) == syscall.ESRCH
	})
}

func TestStoppingAServiceLeavesItStoppedRatherThanStartingItAgain(t *testing.T) {
	h := newHarness(t, 46700, 46799)
	stop := h.start(t)

	defer stop()

	ctx := context.Background()
	execution := h.prepared(t, "exec-01STOP")

	defer func() { _ = h.service.Release(ctx, execution.ID) }()

	record, err := h.service.Start(ctx, execution.ID, held("api", "sleep 300"))
	if err != nil {
		t.Fatalf("start a service: %v", err)
	}

	stopped, err := h.service.Stop(ctx, execution.ID, "api")
	if err != nil {
		t.Fatalf("stop a service: %v", err)
	}

	if stopped.State != entity.ServiceStopped {
		t.Fatalf("a service that was asked to stop is %s", stopped.State)
	}

	time.Sleep(200 * time.Millisecond)

	if h.list(t, execution.ID)[0].State != entity.ServiceStopped {
		t.Fatalf("a service that was asked to stop was started again")
	}

	if syscall.Kill(-record.PID, 0) != syscall.ESRCH {
		t.Fatalf("a service that was asked to stop is still running")
	}
}

func TestServicesOfARunThatIsOverAreRefusedRatherThanStarted(t *testing.T) {
	h := newHarness(t, 46800, 46899)

	execution := h.prepared(t, "exec-01FINISHED")
	execution.State = "failed"

	if err := h.runs.SaveTask(context.Background(), execution); err != nil {
		t.Fatalf("write down a finished run: %v", err)
	}

	if _, err := h.service.Start(
		context.Background(), execution.ID, held("api", "sleep 300"),
	); !errors.Is(err, entity.ErrExecutionRefused) {
		t.Fatalf("starting a service in a finished run answered %v", err)
	}

	if _, err := h.service.Start(
		context.Background(), "exec-01NOTHERE", held("api", "sleep 300"),
	); !errors.Is(err, entity.ErrExecutionUnknown) {
		t.Fatalf("starting a service in a run nothing knows answered %v", err)
	}
}

func itoa(port int) string {
	return strconv.Itoa(port)
}
