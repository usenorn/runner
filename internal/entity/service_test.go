package entity_test

import (
	"errors"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func TestOnlyTheHealthChecksAndServiceStatesThisMachineKnowsAreValid(t *testing.T) {
	for _, kind := range entity.HealthKinds() {
		if !kind.Valid() {
			t.Fatalf("%s is listed as a health check and does not call itself valid", kind)
		}
	}

	if entity.HealthKind("smoke").Valid() {
		t.Fatalf("a health check nothing implements calls itself valid")
	}

	for _, state := range entity.ServiceStates() {
		if !state.Valid() {
			t.Fatalf("%s is listed as a service state and does not call itself valid", state)
		}
	}

	if entity.ServiceState("degraded").Valid() {
		t.Fatalf("a service state nothing reports calls itself valid")
	}
}

func TestOnlyAStartingOrHealthyServiceStillHoldsSomething(t *testing.T) {
	for state, live := range map[entity.ServiceState]bool{
		entity.ServiceStarting:  true,
		entity.ServiceHealthy:   true,
		entity.ServiceUnhealthy: false,
		entity.ServiceStopped:   false,
	} {
		if state.Live() != live {
			t.Fatalf("%s says it is live=%v", state, state.Live())
		}
	}
}

func TestAServiceThatCannotBeStartedSaysWhichPartOfItIsWrong(t *testing.T) {
	good := entity.Service{Name: "api", Command: []string{"sh", "-c", "true"}}

	if err := good.Valid(); err != nil {
		t.Fatalf("a plain service was refused: %v", err)
	}

	for what, service := range map[string]entity.Service{
		"a name with spaces":  {Name: "the api", Command: []string{"true"}},
		"nothing to run":      {Name: "api"},
		"a folder above it":   {Name: "api", Command: []string{"true"}, Dir: "../elsewhere"},
		"needing itself":      {Name: "api", Command: []string{"true"}, Requires: []string{"api"}},
		"an unreadable check": {Name: "api", Command: []string{"true"}, Health: entity.Health{Kind: entity.HealthLog, Pattern: "("}},
	} {
		err := service.Valid()

		if !errors.Is(err, entity.ErrServiceInvalid) {
			t.Fatalf("a service with %s was accepted, and answered %v", what, err)
		}
	}
}

func TestAPortPlaceholderIsFilledInAndAnUnknownOneIsNamedRatherThanLeft(t *testing.T) {
	ports := map[string]int{"api": 43001, "web": 43002}

	filled, err := entity.ResolvePorts("http://127.0.0.1:${ports.api}/v1", ports)
	if err != nil {
		t.Fatalf("fill in a port: %v", err)
	}

	if filled != "http://127.0.0.1:43001/v1" {
		t.Fatalf("the value came back as %q", filled)
	}

	if _, err := entity.ResolvePorts("${ports.database}", ports); !errors.Is(
		err, entity.ErrPortUnknown,
	) {
		t.Fatalf("a placeholder nothing reserves answered %v", err)
	}
}

func TestAServiceAsksForItsOwnPortAndEveryOtherOneItMentions(t *testing.T) {
	service := entity.Service{
		Name:        "web",
		Command:     []string{"pnpm", "dev", "--port", "${ports.web}"},
		Environment: map[string]string{"API_URL": "http://127.0.0.1:${ports.api}"},
		Health:      entity.Health{Kind: entity.HealthHTTP, Path: "/"},
	}

	wanted := service.Ports()

	if len(wanted) != 2 || wanted[0] != "web" || wanted[1] != "api" {
		t.Fatalf("the service asked for %v", wanted)
	}

	quiet := entity.Service{Name: "worker", Command: []string{"true"}}

	if len(quiet.Ports()) != 1 || quiet.Ports()[0] != "worker" {
		t.Fatalf(
			"a service that mentions no port asked for %v; every service gets one so that "+
				"NORN_PORT_WORKER means something",
			quiet.Ports(),
		)
	}
}

func TestAServicesPortReachesItUnderANameAShellCanRead(t *testing.T) {
	for name, variable := range map[string]string{
		"api":       "NORN_PORT_API",
		"web-front": "NORN_PORT_WEB_FRONT",
		"db_2":      "NORN_PORT_DB_2",
	} {
		if held := entity.PortVariable(name); held != variable {
			t.Fatalf("%s reaches its service as %s", name, held)
		}
	}
}
