package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
)

const (
	outdatedCode = "runner_outdated"
	revokedCode  = "runner_revoked"
	detailLimit  = 4 << 10
)

type websocketChannel struct {
	server string
	agent  string
	cfg    config.Channel
}

func New(runner config.Runner, app config.App, cfg config.Channel) repository.Channel {
	return &websocketChannel{
		server: runner.Server,
		agent:  "norn-runner/" + app.Version,
		cfg:    cfg,
	}
}

func (r *websocketChannel) Dial(
	ctx context.Context,
	ticket, version string,
) (repository.Conn, error) {
	target, err := r.target(ticket, version)
	if err != nil {
		return nil, err
	}

	handshake, settled := context.WithTimeout(ctx, r.cfg.HandshakeTimeout)
	defer settled()

	socket, response, err := websocket.Dial(handshake, target, &websocket.DialOptions{
		HTTPHeader: http.Header{"User-Agent": []string{r.agent}},
	})
	if err != nil {
		return nil, r.refusal(response, err)
	}

	socket.SetReadLimit(r.cfg.MaxMessageBytes)

	return &connection{socket: socket, cfg: r.cfg}, nil
}

func (r *websocketChannel) target(ticket, version string) (string, error) {
	parsed, err := url.Parse(strings.TrimSuffix(r.server, "/"))
	if err != nil {
		return "", fmt.Errorf("read the norn address %q: %w", r.server, err)
	}

	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	}

	parsed.Path += entity.ChannelPath
	parsed.RawQuery = url.Values{"ticket": {ticket}, "version": {version}}.Encode()

	return parsed.String(), nil
}

func (r *websocketChannel) refusal(response *http.Response, err error) error {
	if response == nil {
		return r.unreachable(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(response.Body, detailLimit))

	switch response.StatusCode {
	case http.StatusUpgradeRequired:
		return entity.OutdatedError{Detail: detailOf(body)}
	case http.StatusNotFound:
		return entity.ErrChannelOff
	case http.StatusForbidden:
		return entity.ErrAgentDisabled
	case http.StatusUnauthorized:
		if codeOf(body) == revokedCode {
			return entity.ErrRunnerRevoked
		}

		return entity.ErrCredentialInvalid
	default:
		if codeOf(body) == outdatedCode {
			return entity.OutdatedError{Detail: detailOf(body)}
		}

		return fmt.Errorf("norn answered %s when this machine opened its channel", response.Status)
	}
}

func (r *websocketChannel) unreachable(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return fmt.Errorf("%w at %s: %w", entity.ErrServerUnreachable, r.server, err)
}

type connection struct {
	socket *websocket.Conn
	cfg    config.Channel
}

func (c *connection) Read(ctx context.Context) (channelv1.Envelope, error) {
	kind, raw, err := c.socket.Read(ctx)
	if err != nil {
		return channelv1.Envelope{}, closure(err)
	}

	if kind != websocket.MessageText {
		return channelv1.Envelope{}, channelv1.ErrEnvelopeInvalid
	}

	var envelope channelv1.Envelope

	if err := json.Unmarshal(raw, &envelope); err != nil {
		return channelv1.Envelope{}, channelv1.ErrEnvelopeInvalid
	}

	if envelope.V != channelv1.Version {
		return channelv1.Envelope{}, channelv1.ErrEnvelopeInvalid
	}

	return envelope, nil
}

func (c *connection) Write(ctx context.Context, envelope channelv1.Envelope) error {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode a %s: %w", envelope.Type, err)
	}

	ctx, settled := context.WithTimeout(ctx, c.cfg.WriteTimeout)
	defer settled()

	if err := c.socket.Write(ctx, websocket.MessageText, raw); err != nil {
		return closure(err)
	}

	return nil
}

func (c *connection) Close() error {
	return c.socket.Close(websocket.StatusNormalClosure, "")
}

func closure(err error) error {
	status := websocket.CloseStatus(err)

	switch status {
	case -1:
		return err
	case websocket.StatusPolicyViolation:
		if strings.Contains(err.Error(), "revoked") {
			return entity.ErrRunnerRevoked
		}

		return entity.ErrChannelDisplaced
	case websocket.StatusUnsupportedData:
		return fmt.Errorf("%w: %s", entity.ErrChannelUnsupported, err)
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return entity.ErrChannelClosed
	default:
		return fmt.Errorf("%w: %s", entity.ErrChannelClosed, err)
	}
}

func detailOf(body []byte) string {
	var problem struct {
		Detail string `json:"detail"`
	}

	if err := json.Unmarshal(body, &problem); err != nil {
		return ""
	}

	return problem.Detail
}

func codeOf(body []byte) string {
	var problem struct {
		Code string `json:"code"`
	}

	if err := json.Unmarshal(body, &problem); err != nil {
		return ""
	}

	return problem.Code
}
