package channel_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
)

func TestTheFirstThingAMachineSaysIsWhichBuildItIsAndWhatItHolds(t *testing.T) {
	h := newHarness(t, true, 1)
	stop := h.start(t)

	defer stop()

	w := h.awaitDial(t)

	hello := w.await(t, channelv1.RunnerHello)

	var greeting channelv1.Hello

	if err := json.Unmarshal(hello.Payload, &greeting); err != nil {
		t.Fatalf("read the greeting: %v", err)
	}

	if greeting.Version != "1.4.0" || greeting.Protocol != entity.ChannelProtocol {
		t.Fatalf("the greeting reads %+v", greeting)
	}

	if greeting.Capacity != 2 {
		t.Fatalf("the greeting offers %d slots, want 2", greeting.Capacity)
	}
}

func TestAMachineKeepsTellingNornItIsAliveSoItsLeasesHold(t *testing.T) {
	h := newHarness(t, true, 1)
	stop := h.start(t)

	defer stop()

	w := h.awaitDial(t)

	for range 2 {
		beat := w.await(t, channelv1.RunnerHeartbeat)

		var pulse channelv1.Pulse

		if err := json.Unmarshal(beat.Payload, &pulse); err != nil {
			t.Fatalf("read the heartbeat: %v", err)
		}

		if pulse.Capacity != 2 {
			t.Fatalf("the heartbeat reads %+v", pulse)
		}
	}
}

func TestEverythingNornSendsIsAnsweredSoItStopsResending(t *testing.T) {
	h := newHarness(t, true, 1)
	stop := h.start(t)

	defer stop()

	w := h.awaitDial(t)

	w.send(t, channelv1.Sync, "", []byte(`{"executions":[]}`))

	answered := w.await(t, channelv1.Ack)

	if answered.AckID == "" {
		t.Fatalf("the machine answered %+v with no message named", answered)
	}
}

func TestKillingTheConnectionMidFlightLosesNoEventAndDuplicatesNone(t *testing.T) {
	h := newHarness(t, false, 2)

	written := h.queue(t, 6)

	stop := h.start(t)

	defer stop()

	first := h.awaitDial(t)

	_ = first.await(t, channelv1.RunnerHello)

	delivered := map[string]int{}

	stranded := first.await(t, channelv1.ExecutionEvent)
	delivered[stranded.ID]++

	first.hangUp(entity.ErrChannelClosed)

	second := h.awaitDial(t)

	_ = second.await(t, channelv1.RunnerHello)

	deadline := time.After(10 * time.Second)

	for len(delivered) < len(written) {
		select {
		case envelope := <-second.outbound:
			if channelv1.MessageType(envelope.Type) != channelv1.ExecutionEvent {
				continue
			}

			delivered[envelope.ID]++

			second.inbound <- channelv1.Acknowledgement(envelope.ID, time.Now().UTC())
		case <-deadline:
			t.Fatalf(
				"after reconnecting the machine delivered %d of %d events",
				len(delivered), len(written),
			)
		}
	}

	for _, id := range written {
		if delivered[id] == 0 {
			t.Fatalf("%s never reached norn", id)
		}
	}

	if delivered[stranded.ID] != 2 {
		t.Fatalf(
			"the event that was in flight when the connection died arrived %d times; norn "+
				"deduplicates, so it must be sent again rather than dropped",
			delivered[stranded.ID],
		)
	}

	for id, count := range delivered {
		if id != stranded.ID && count != 1 {
			t.Fatalf("%s reached norn %d times", id, count)
		}
	}

	h.awaitEmptySpool(t)
}

func TestEventsAreDeliveredInTheOrderTheyHappened(t *testing.T) {
	h := newHarness(t, true, 1)

	written := h.queue(t, 5)

	stop := h.start(t)

	defer stop()

	w := h.awaitDial(t)

	_ = w.await(t, channelv1.RunnerHello)

	for _, id := range written {
		if delivered := w.await(t, channelv1.ExecutionEvent); delivered.ID != id {
			t.Fatalf("norn was sent %s, want %s next", delivered.ID, id)
		}
	}
}

func TestAMessageNornSendsTwiceIsActedOnOnce(t *testing.T) {
	h := newHarness(t, true, 1)
	stop := h.start(t)

	defer stop()

	w := h.awaitDial(t)

	_ = w.await(t, channelv1.RunnerHello)

	offer, err := channelv1.NewServerMessage(
		channelv1.ExecutionOffer, "exec-01ABC",
		[]byte(`{"execution_id":"exec-01ABC","reference":"NORN-45"}`), time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("build an offer: %v", err)
	}

	for range 2 {
		w.inbound <- channelv1.Frame(offer)
	}

	accepted := 0
	deadline := time.After(2 * time.Second)

	for accepted < 1 {
		select {
		case envelope := <-w.outbound:
			if channelv1.MessageType(envelope.Type) == channelv1.ExecutionAccepted {
				accepted++
			}
		case <-deadline:
			t.Fatalf("the machine never accepted the offer")
		}
	}

	settling := time.After(500 * time.Millisecond)

	for {
		select {
		case envelope := <-w.outbound:
			if channelv1.MessageType(envelope.Type) == channelv1.ExecutionAccepted {
				t.Fatalf("a redelivered offer was accepted a second time")
			}
		case <-settling:
			return
		}
	}
}

func TestARunnerNornWillNotTalkToStopsTryingAndSaysWhy(t *testing.T) {
	h := newHarness(t, true, 0)

	detail := "this runner is 0.9.0 and norn needs 1.2.0 or newer"
	h.sessions.err = entity.OutdatedError{Detail: detail}

	stop := h.start(t)

	defer stop()

	report := awaitState(t, h, entity.ChannelRefused)

	if report.Detail != detail {
		t.Fatalf("the machine says %q, want norn's own words", report.Detail)
	}

	handouts := h.sessions.handouts()

	time.Sleep(200 * time.Millisecond)

	if again := h.sessions.handouts(); again != handouts {
		t.Fatalf("a refused machine kept trying: %d attempts became %d", handouts, again)
	}
}

func TestAMachineThatCannotReachNornKeepsTrying(t *testing.T) {
	h := newHarness(t, true, 0)
	h.sessions.err = entity.ErrServerUnreachable

	stop := h.start(t)

	defer stop()

	_ = awaitState(t, h, entity.ChannelOffline)

	handouts := h.sessions.handouts()

	time.Sleep(200 * time.Millisecond)

	if again := h.sessions.handouts(); again <= handouts {
		t.Fatalf("an unreachable norn stopped being tried after %d attempts", handouts)
	}
}

func TestAChannelSwitchedOffInConfigurationNeverDials(t *testing.T) {
	h := newHarness(t, true, 1)
	h.service = offChannel(t, h)

	stop := h.start(t)

	defer stop()

	time.Sleep(200 * time.Millisecond)

	if h.sessions.handouts() != 0 {
		t.Fatalf("a channel switched off still asked for a ticket")
	}

	if state := h.service.Report(context.Background()).State; state != entity.ChannelOff {
		t.Fatalf("a channel switched off reports %q", state)
	}
}

func TestWhatIsWaitingForNornIsCountedInTheReport(t *testing.T) {
	h := newHarness(t, true, 0)
	h.sessions.err = entity.ErrServerUnreachable

	h.queue(t, 3)

	if waiting := h.service.Report(context.Background()).Waiting; waiting != 3 {
		t.Fatalf("the machine says %d events are waiting, want 3", waiting)
	}
}

func TestAnEventThatWaitedTooLongForNornIsDroppedRatherThanKeptForever(t *testing.T) {
	h := newHarness(t, true, 0)
	h.sessions.err = errors.New("norn is down")

	h.queueAged(t, 2, 48*time.Hour)

	stop := h.start(t)

	defer stop()

	deadline := time.After(5 * time.Second)

	for {
		if h.service.Report(context.Background()).Waiting == 0 {
			return
		}

		select {
		case <-deadline:
			t.Fatalf("events older than the machine keeps them were never dropped")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func awaitState(t *testing.T, h *harness, want entity.ChannelState) entity.ChannelReport {
	t.Helper()

	deadline := time.After(5 * time.Second)

	for {
		report := h.service.Report(context.Background())

		if report.State == want {
			return report
		}

		select {
		case <-deadline:
			t.Fatalf("the channel is %q, want %q", report.State, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
