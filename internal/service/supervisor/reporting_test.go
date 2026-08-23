package supervisor_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func TestNornIsToldWhichServiceItIsOnWhichPortAndHowItIsChecked(t *testing.T) {
	h := newHarness(t, 46100, 46199)
	stop := h.start(t)

	defer stop()

	execution := h.prepared(t, "exec-01REPORTED")

	record, err := h.service.Start(context.Background(), execution.ID, entity.Service{
		Name:    "api",
		Command: []string{"sh", "-c", "echo listening on $NORN_PORT_API; sleep 300"},
		Health:  entity.Health{Kind: entity.HealthLog, Pattern: "listening on"},
	})
	if err != nil {
		t.Fatalf("start the api: %v", err)
	}

	h.awaitReported(t, execution.ID, "api is healthy")

	reported := h.serviceReports(t, execution.ID)
	if len(reported) == 0 {
		t.Fatal(
			"nothing went up as a service report, so norn would have only a sentence and no way " +
				"to say what is running, on which port, or how it is checked",
		)
	}

	last := reported[len(reported)-1]

	if last.Name != "api" {
		t.Fatalf("the report names %q rather than the service that changed", last.Name)
	}

	if last.State != channelv1.ServiceHealthy {
		t.Fatalf("the last report says %q rather than healthy", last.State)
	}

	if last.Port != record.Port {
		t.Fatalf(
			"the report carries port %d while the run reserved %d. A port is held by the run, so "+
				"what norn shows has to be the one anything was told to use",
			last.Port, record.Port,
		)
	}

	if last.Probe != channelv1.ProbeLog {
		t.Fatalf(
			"the report says it is checked by %q rather than by what it prints, which is how this "+
				"service was described",
			last.Probe,
		)
	}
}

func TestWhatAServicePrintsReachesTheRunsOutput(t *testing.T) {
	h := newHarness(t, 46200, 46299)
	stop := h.start(t)

	defer stop()

	execution := h.prepared(t, "exec-01FORWARDED")

	if _, err := h.service.Start(context.Background(), execution.ID, entity.Service{
		Name:    "api",
		Command: []string{"sh", "-c", "echo listening on 1; echo second line; sleep 300"},
		Health:  entity.Health{Kind: entity.HealthLog, Pattern: "listening on"},
	}); err != nil {
		t.Fatalf("start the api: %v", err)
	}

	h.await(t, "waited for the api's own output to be sent up", func() bool {
		for _, line := range h.lines("api") {
			if strings.Contains(line.Text, "second line") {
				return true
			}
		}

		return false
	})

	for _, line := range h.lines("api") {
		if line.Source != "api" {
			t.Fatalf(
				"a line came up under source %q. The services panel reads its output by the "+
					"service's own name, so anything else is invisible there",
				line.Source,
			)
		}
	}
}

func (h *harness) serviceReports(t *testing.T, executionID string) []channelv1.Service {
	t.Helper()

	waiting, err := h.spool.Head(context.Background(), 0)
	if err != nil {
		t.Fatalf("read the spool: %v", err)
	}

	reported := []channelv1.Service{}

	for _, message := range waiting {
		if message.Type != channelv1.ServiceState || message.ExecutionID != executionID {
			continue
		}

		var running channelv1.Service

		if err := json.Unmarshal(message.Payload, &running); err != nil {
			t.Fatalf("read a service report: %v", err)
		}

		reported = append(reported, running)
	}

	return reported
}
