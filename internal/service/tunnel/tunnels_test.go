package tunnel_test

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
	"github.com/usenorn/runner/internal/entity"
)

func local(t *testing.T, answer string) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start a local service: %v", err)
	}

	server := &http.Server{
		ReadHeaderTimeout: settle,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, answer+" "+r.URL.Path)
		}),
	}

	go func() { _ = server.Serve(listener) }()

	t.Cleanup(func() { _ = server.Close() })

	return listener.Addr().(*net.TCPAddr).Port
}

func ask(t *testing.T, stream *channelv1.Stream, path string) string {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, "http://preview"+path, nil)
	if err != nil {
		t.Fatalf("build a request: %v", err)
	}

	if err := request.Write(stream); err != nil {
		t.Fatalf("write a request down the stream: %v", err)
	}

	if err := stream.SetReadDeadline(time.Now().Add(settle)); err != nil {
		t.Fatalf("set a deadline: %v", err)
	}

	response, err := http.ReadResponse(bufio.NewReader(stream), request)
	if err != nil {
		t.Fatalf("read the response: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read the body: %v", err)
	}

	return string(body)
}

func TestAStreamNamingAPreviewThisMachineHoldsReachesTheServiceBehindIt(t *testing.T) {
	h := newHarness(t, settings())
	port := local(t, "served")

	h.running("web", port, entity.ServiceHealthy)
	h.exposed("web")

	session := h.attached()
	stream := session.open(t, channelv1.StreamOpen{Execution: testExecution, Preview: "web"})

	if answered := session.ready(t, stream); !answered.Open {
		t.Fatalf("the machine refused a preview it is running: %s", answered.Reason)
	}

	if body := ask(t, stream, "/dashboard"); !strings.Contains(body, "served /dashboard") {
		t.Fatalf(
			"the service answered %q; the browser has to reach the process itself, byte for "+
				"byte, or nothing about a live app works through the tunnel",
			body,
		)
	}
}

func TestAStreamNamingAnExecutionThisMachineDoesNotHoldIsRefusedBeforeAnyDial(t *testing.T) {
	h := newHarness(t, settings())
	port := local(t, "served")

	h.running("web", port, entity.ServiceHealthy)
	h.exposed("web")

	session := h.attached()
	stream := session.open(t, channelv1.StreamOpen{
		Execution: "exec-SOMEBODY-ELSES",
		Preview:   "web",
	})

	answered := session.ready(t, stream)

	if answered.Open {
		t.Fatal(
			"a stream naming another run was carried through; a gateway that could be talked " +
				"into any pair would reach any port on this machine",
		)
	}
}

func TestAStreamNamingAPreviewThatWasClosedIsRefused(t *testing.T) {
	h := newHarness(t, settings())
	port := local(t, "served")

	h.running("web", port, entity.ServiceHealthy)
	h.exposed("web")

	if _, err := h.previews.Close(t.Context(), testExecution, "web"); err != nil {
		t.Fatalf("close the preview: %v", err)
	}

	session := h.attached()
	stream := session.open(t, channelv1.StreamOpen{Execution: testExecution, Preview: "web"})

	if answered := session.ready(t, stream); answered.Open {
		t.Fatal(
			"a preview somebody closed still answered; closing has to stop the address at once " +
				"or it is not a way to take a preview back",
		)
	}
}

func TestAServiceThatStoppedBeingHealthyStopsAnsweringItsPreview(t *testing.T) {
	h := newHarness(t, settings())
	port := local(t, "served")

	h.running("web", port, entity.ServiceHealthy)
	h.exposed("web")
	h.running("web", port, entity.ServiceStopped)

	session := h.attached()
	stream := session.open(t, channelv1.StreamOpen{Execution: testExecution, Preview: "web"})

	if answered := session.ready(t, stream); answered.Open {
		t.Fatal(
			"a preview whose service has stopped still answered; a reviewer would get a " +
				"connection refused from somebody else's laptop instead of a page saying so",
		)
	}
}

func TestOneMachineCarriesOnlyAsManyStreamsAsItsConfigurationAllows(t *testing.T) {
	cfg := settings()
	cfg.MaxStreams = 1

	h := newHarness(t, cfg)
	port := local(t, "served")

	h.running("web", port, entity.ServiceHealthy)
	h.exposed("web")

	session := h.attached()

	first := session.open(t, channelv1.StreamOpen{Execution: testExecution, Preview: "web"})
	if answered := session.ready(t, first); !answered.Open {
		t.Fatalf("the first stream was refused: %s", answered.Reason)
	}

	second := session.open(t, channelv1.StreamOpen{Execution: testExecution, Preview: "web"})

	if answered := session.ready(t, second); answered.Open {
		t.Fatal(
			"a stream past this machine's cap was carried anyway; without a cap one page of " +
				"images could take every connection the machine has",
		)
	}
}

func TestTheTunnelIsReportedSoAPersonCanSeeWhetherPreviewsReachAnybody(t *testing.T) {
	h := newHarness(t, settings())

	h.attached()

	deadline := time.Now().Add(settle)

	for time.Now().Before(deadline) {
		if h.tunnels.Report().State == entity.TunnelLive {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf(
		"the tunnel is held but reported as %q; norn runner status is where somebody looks "+
			"when a preview link does not open",
		h.tunnels.Report().State,
	)
}

func TestATunnelThatDropsIsOpenedAgainWithoutTouchingTheExecution(t *testing.T) {
	h := newHarness(t, settings())
	port := local(t, "served")

	h.running("web", port, entity.ServiceHealthy)
	h.exposed("web")

	first := h.attached()

	stream := first.open(t, channelv1.StreamOpen{Execution: testExecution, Preview: "web"})
	if answered := first.ready(t, stream); !answered.Open {
		t.Fatalf("the first stream was refused: %s", answered.Reason)
	}

	_ = first.Close()

	second := h.attached()

	again := second.open(t, channelv1.StreamOpen{Execution: testExecution, Preview: "web"})

	if answered := second.ready(t, again); !answered.Open {
		t.Fatalf(
			"after the tunnel dropped and came back, a preview the machine still holds was "+
				"refused (%s); a reconnect must not cost the run anything",
			answered.Reason,
		)
	}

	if body := ask(t, again, "/"); !strings.Contains(body, "served /") {
		t.Fatalf("the service answered %q after the tunnel came back", body)
	}
}

func TestAMachineWhoseNornServesNoPreviewDomainStopsTryingRatherThanLooping(t *testing.T) {
	h := newRefusedHarness(t, settings(), entity.ErrPreviewsUnserved)

	settled := false
	deadline := time.Now().Add(settle)

	for time.Now().Before(deadline) {
		if h.tunnels.Report().State == entity.TunnelUnserved {
			settled = true

			break
		}

		time.Sleep(time.Millisecond)
	}

	if !settled {
		t.Fatalf(
			"the tunnel is reported as %q against a norn that serves no preview domain",
			h.tunnels.Report().State,
		)
	}

	time.Sleep(15 * settings().RetryMin)

	if dials := h.dialled(); dials > 3 {
		t.Fatalf(
			"this machine dialled a gateway that does not exist %d times; an instance serving "+
				"no preview domain is a settled answer, not an outage, and retrying it for "+
				"ever is a request every few seconds that can never work",
			dials,
		)
	}
}
