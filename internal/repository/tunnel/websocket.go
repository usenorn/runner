package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

const detailLimit = 4 << 10

type websocketTunnel struct {
	agent string
	cfg   config.Tunnel
}

func New(app config.App, cfg config.Tunnel) repository.Tunnel {
	return &websocketTunnel{agent: "norn-runner/" + app.Version, cfg: cfg}
}

func (r *websocketTunnel) Dial(
	ctx context.Context,
	ticket entity.TunnelTicket,
) (repository.TunnelSession, error) {
	target, err := address(ticket)
	if err != nil {
		return nil, err
	}

	handshake, settled := context.WithTimeout(ctx, r.cfg.HandshakeTimeout)
	defer settled()

	socket, response, err := websocket.Dial(handshake, target, &websocket.DialOptions{
		HTTPHeader: http.Header{"User-Agent": []string{r.agent}},
	})
	if err != nil {
		return nil, refusal(response, err)
	}

	settings := yamux.DefaultConfig()
	settings.KeepAliveInterval = r.cfg.Heartbeat
	settings.ConnectionWriteTimeout = r.cfg.WriteTimeout
	settings.LogOutput = io.Discard

	carried := websocket.NetConn(context.WithoutCancel(ctx), socket, websocket.MessageBinary)

	session, err := yamux.Client(carried, settings)
	if err != nil {
		_ = socket.CloseNow()

		return nil, fmt.Errorf("open the tunnel session: %w", err)
	}

	return &held{socket: socket, session: session}, nil
}

func address(ticket entity.TunnelTicket) (string, error) {
	parsed, err := url.Parse(strings.TrimSuffix(ticket.Previews.Gateway, "/"))
	if err != nil {
		return "", fmt.Errorf("read the gateway address %q: %w", ticket.Previews.Gateway, err)
	}

	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	}

	parsed.Path += channelv1.TunnelPath
	parsed.RawQuery = url.Values{"ticket": {ticket.Ticket}}.Encode()

	return parsed.String(), nil
}

func refusal(response *http.Response, err error) error {
	if response == nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		return entity.UnreachableError{Detail: entity.Redacted(err.Error())}
	}

	defer func() { _ = response.Body.Close() }()

	_, _ = io.ReadAll(io.LimitReader(response.Body, detailLimit))

	switch response.StatusCode {
	case http.StatusUnauthorized:
		return entity.ErrCredentialInvalid
	case http.StatusForbidden:
		return entity.ErrAgentDisabled
	case http.StatusNotFound:
		return entity.ErrPreviewsUnserved
	default:
		return entity.UnreachableError{
			Detail: "the gateway answered " + response.Status,
		}
	}
}

type held struct {
	socket  *websocket.Conn
	session *yamux.Session
}

func (h *held) Accept() (net.Conn, error) {
	return h.session.AcceptStream()
}

func (h *held) Close() error {
	_ = h.session.Close()

	return h.socket.CloseNow()
}
