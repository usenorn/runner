package channel_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/repository/channel"
)

type stand struct {
	server  *httptest.Server
	asked   chan url.Values
	paths   chan string
	headers chan http.Header
}

func newStand(t *testing.T, serve func(*websocket.Conn, *http.Request)) (*stand, repository.Channel) {
	t.Helper()

	held := &stand{
		asked:   make(chan url.Values, 4),
		paths:   make(chan string, 4),
		headers: make(chan http.Header, 4),
	}

	held.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		held.asked <- r.URL.Query()
		held.paths <- r.URL.Path
		held.headers <- r.Header.Clone()

		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = socket.CloseNow() }()

		serve(socket, r)
	}))

	t.Cleanup(held.server.Close)

	return held, channel.New(
		config.Runner{Server: held.server.URL},
		config.App{Version: "1.4.0"},
		settings(),
	)
}

func settings() config.Channel {
	return config.Channel{
		Enabled:          true,
		HandshakeTimeout: 5 * time.Second,
		Heartbeat:        15 * time.Second,
		WriteTimeout:     5 * time.Second,
		RetryMin:         time.Second,
		RetryMax:         time.Minute,
		MaxMessageBytes:  1 << 20,
	}
}

func refusing(t *testing.T, status int, body any) repository.Channel {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)

		_ = json.NewEncoder(w).Encode(body)
	}))

	t.Cleanup(server.Close)

	return channel.New(config.Runner{Server: server.URL}, config.App{Version: "1.4.0"}, settings())
}

func TestTheChannelIsOpenedWithATicketAndTheBuildTheMachineIsRunning(t *testing.T) {
	held, dialler := newStand(t, func(socket *websocket.Conn, _ *http.Request) {
		<-time.After(50 * time.Millisecond)
		_ = socket.Close(websocket.StatusNormalClosure, "")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialler.Dial(ctx, "nrt_good", "1.4.0")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer func() { _ = conn.Close() }()

	asked := <-held.asked

	if asked.Get("ticket") != "nrt_good" {
		t.Fatalf("the channel was opened with ticket %q", asked.Get("ticket"))
	}

	if asked.Get("version") != "1.4.0" {
		t.Fatalf("the channel named version %q, so norn cannot judge this build", asked.Get("version"))
	}

	if origin := (<-held.headers).Get("Origin"); origin != "" {
		t.Fatalf("the dial carried Origin %q, which norn refuses the handshake over", origin)
	}
}

func TestTheChannelPathIsKeptWhenNornLivesUnderAPrefix(t *testing.T) {
	held, _ := newStand(t, func(socket *websocket.Conn, _ *http.Request) {
		_ = socket.Close(websocket.StatusNormalClosure, "")
	})

	dialler := channel.New(
		config.Runner{Server: held.server.URL + "/norn/"},
		config.App{Version: "1.4.0"},
		settings(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialler.Dial(ctx, "nrt_good", "1.4.0")
	if err == nil {
		_ = conn.Close()
	}

	<-held.asked

	select {
	case path := <-held.paths:
		if path != "/norn"+entity.ChannelPath {
			t.Fatalf("the dial asked for %q, want the channel under norn's own prefix", path)
		}
	case <-time.After(time.Second):
		t.Fatalf("the dial never reached the server")
	}
}

func TestMessagesGoOutAndComeBackAsTheEnvelopeNornSpeaks(t *testing.T) {
	_, dialler := newStand(t, func(socket *websocket.Conn, _ *http.Request) {
		ctx := context.Background()

		_, raw, err := socket.Read(ctx)
		if err != nil {
			return
		}

		var envelope channelv1.Envelope

		if err := json.Unmarshal(raw, &envelope); err != nil {
			return
		}

		answer, _ := json.Marshal(channelv1.Acknowledgement(envelope.ID, time.Now().UTC()))

		_ = socket.Write(ctx, websocket.MessageText, answer)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialler.Dial(ctx, "nrt_good", "1.4.0")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer func() { _ = conn.Close() }()

	message, err := channelv1.NewRunnerMessage(
		channelv1.RunnerHello, "", []byte(`{"version":"1.4.0"}`), time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("build a hello: %v", err)
	}

	if err := conn.Write(ctx, channelv1.Frame(message)); err != nil {
		t.Fatalf("write: %v", err)
	}

	answer, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !answer.Acknowledging() || answer.AckID != message.ID {
		t.Fatalf("norn answered %+v, want an ack for %s", answer, message.ID)
	}
}

func TestAFrameFromAnotherProtocolVersionIsNotRead(t *testing.T) {
	_, dialler := newStand(t, func(socket *websocket.Conn, _ *http.Request) {
		raw, _ := json.Marshal(channelv1.Envelope{
			V: channelv1.Version + 1, ID: "01ABC", Type: string(channelv1.Sync),
		})

		_ = socket.Write(context.Background(), websocket.MessageText, raw)

		<-time.After(time.Second)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialler.Dial(ctx, "nrt_good", "1.4.0")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer func() { _ = conn.Close() }()

	if _, err := conn.Read(ctx); !errors.Is(err, channelv1.ErrEnvelopeInvalid) {
		t.Fatalf("a frame from another protocol version read as %v", err)
	}
}

func TestEveryWayNornCanEndTheChannelIsSaidInTheRunnersOwnWords(t *testing.T) {
	cases := map[string]struct {
		status websocket.StatusCode
		reason string
		want   error
	}{
		"displaced": {
			status: websocket.StatusPolicyViolation,
			reason: "another connection took this channel",
			want:   entity.ErrChannelDisplaced,
		},
		"revoked": {
			status: websocket.StatusPolicyViolation,
			reason: "this runner has been revoked",
			want:   entity.ErrRunnerRevoked,
		},
		"a message norn refused": {
			status: websocket.StatusUnsupportedData,
			reason: "runner channel message type is not recognised",
			want:   entity.ErrChannelUnsupported,
		},
		"a clean goodbye": {
			status: websocket.StatusNormalClosure,
			want:   entity.ErrChannelClosed,
		},
		"norn falling over": {
			status: websocket.StatusInternalError,
			want:   entity.ErrChannelClosed,
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, dialler := newStand(t, func(socket *websocket.Conn, _ *http.Request) {
				_ = socket.Close(want.status, want.reason)
			})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, err := dialler.Dial(ctx, "nrt_good", "1.4.0")
			if err != nil {
				t.Fatalf("dial: %v", err)
			}

			defer func() { _ = conn.Close() }()

			if _, err := conn.Read(ctx); !errors.Is(err, want.want) {
				t.Fatalf("norn closing with %d read as %v, want %v", want.status, err, want.want)
			}
		})
	}
}

func TestARefusalBeforeTheUpgradeIsReadAsWhatItActuallyIs(t *testing.T) {
	detail := "this runner is 0.9.0 and norn needs 1.2.0 or newer. Take the new one with: curl"

	cases := map[string]struct {
		status int
		body   map[string]any
		want   error
	}{
		"too old": {
			status: http.StatusUpgradeRequired,
			body:   map[string]any{"code": "runner_outdated", "detail": detail},
			want:   entity.ErrRunnerOutdated,
		},
		"revoked": {
			status: http.StatusUnauthorized,
			body:   map[string]any{"code": "runner_revoked"},
			want:   entity.ErrRunnerRevoked,
		},
		"a spent ticket": {
			status: http.StatusUnauthorized,
			body:   map[string]any{"code": "runner_credential_invalid"},
			want:   entity.ErrCredentialInvalid,
		},
		"a disabled agent": {
			status: http.StatusForbidden,
			body:   map[string]any{},
			want:   entity.ErrAgentDisabled,
		},
		"the channel switched off": {
			status: http.StatusNotFound,
			body:   map[string]any{},
			want:   entity.ErrChannelOff,
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			dialler := refusing(t, want.status, want.body)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, err := dialler.Dial(ctx, "nrt_stale", "0.9.0")
			if err == nil {
				_ = conn.Close()

				t.Fatalf("a refused dial still opened a channel")
			}

			if !errors.Is(err, want.want) {
				t.Fatalf("norn answering %d read as %v, want %v", want.status, err, want.want)
			}
		})
	}
}

func TestARefusalForBeingTooOldCarriesNornsOwnAdviceBack(t *testing.T) {
	detail := "this runner is 0.9.0 and norn needs 1.2.0 or newer. Take the new one with: curl"

	dialler := refusing(t, http.StatusUpgradeRequired, map[string]any{
		"code": "runner_outdated", "detail": detail,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := dialler.Dial(ctx, "nrt_stale", "0.9.0")
	if err == nil {
		t.Fatal("an outdated build still opened a channel")
	}

	if err.Error() != detail {
		t.Fatalf("the refusal reads %q, want norn's own advice", err.Error())
	}
}

func TestANornThatCannotBeReachedIsNotMistakenForOneThatRefused(t *testing.T) {
	dialler := channel.New(
		config.Runner{Server: "http://127.0.0.1:1"},
		config.App{Version: "1.4.0"},
		settings(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := dialler.Dial(ctx, "nrt_good", "1.4.0")
	if !errors.Is(err, entity.ErrServerUnreachable) {
		t.Fatalf("a dead address read as %v, want an unreachable norn", err)
	}
}
