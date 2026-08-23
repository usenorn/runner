package channel

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/repository"
)

const (
	outbound   = 32
	drainPause = 250 * time.Millisecond
)

type connection struct {
	service *channelsService
	conn    repository.Conn

	writes chan channelv1.Envelope

	mu       sync.Mutex
	seen     map[string]time.Time
	inflight string
	settled  chan struct{}
}

func (c *connection) run(ctx context.Context) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	if err := c.greet(ctx); err != nil {
		return err
	}

	finished := make(chan error, 3)

	go func() { finished <- c.readPump(ctx) }()
	go func() { finished <- c.writePump(ctx) }()
	go func() { finished <- c.spoolPump(ctx) }()

	err := <-finished

	stop()

	return err
}

func (c *connection) greet(ctx context.Context) error {
	raw, err := json.Marshal(c.service.executions.Greeting())
	if err != nil {
		return err
	}

	message, err := channelv1.NewRunnerMessage(
		channelv1.RunnerHello, "", raw, c.service.now(),
	)
	if err != nil {
		return err
	}

	return c.conn.Write(ctx, channelv1.Frame(message))
}

func (c *connection) readPump(ctx context.Context) error {
	for {
		envelope, err := c.conn.Read(ctx)
		if err != nil {
			return err
		}

		c.service.heard()

		if envelope.Acknowledging() {
			c.settle(envelope.AckID)

			continue
		}

		message := envelope.Message()

		if err := channelv1.ValidateOutbound(message); err != nil {
			logging.From(ctx).WarnContext(
				ctx,
				"norn sent something this machine does not understand",
				slog.String("type", envelope.Type),
				slog.String("error", err.Error()),
			)

			continue
		}

		if !c.remember(message.ID) {
			if err := c.acknowledge(ctx, message.ID); err != nil {
				return err
			}

			continue
		}

		if err := c.act(ctx, message); err != nil {
			return err
		}

		if err := c.acknowledge(ctx, message.ID); err != nil {
			return err
		}
	}
}

func (c *connection) act(ctx context.Context, message channelv1.Message) error {
	switch message.Type {
	case channelv1.Sync:
		var leased channelv1.Leased

		if err := json.Unmarshal(message.Payload, &leased); err != nil {
			return c.unreadable(ctx, message, err)
		}

		return c.service.executions.Reconcile(ctx, leased.Executions)
	case channelv1.ExecutionOffer:
		var offer channelv1.Offer

		if err := json.Unmarshal(message.Payload, &offer); err != nil {
			return c.unreadable(ctx, message, err)
		}

		return c.service.executions.Offer(ctx, offer)
	case channelv1.ExecutionStart:
		var start channelv1.Start

		if err := json.Unmarshal(message.Payload, &start); err != nil {
			return c.unreadable(ctx, message, err)
		}

		return c.service.executions.Start(ctx, message.ExecutionID, start)
	case channelv1.ExecutionCancel:
		var cancellation channelv1.Cancellation

		if err := json.Unmarshal(message.Payload, &cancellation); err != nil {
			return c.unreadable(ctx, message, err)
		}

		return c.service.executions.Cancel(ctx, message.ExecutionID, cancellation.Reason)
	case channelv1.ExecutionResume:
		var instruction channelv1.Instruction

		if err := json.Unmarshal(message.Payload, &instruction); err != nil {
			return c.unreadable(ctx, message, err)
		}

		return c.service.executions.Continue(ctx, message.ExecutionID, instruction)
	case channelv1.QuestionAnswered:
		var answer channelv1.Answer

		if err := json.Unmarshal(message.Payload, &answer); err != nil {
			return c.unreadable(ctx, message, err)
		}

		return c.service.questions.Answered(ctx, message.ExecutionID, entity.Answer{
			QuestionID: answer.QuestionID,
			Ref:        answer.Ref,
			Answer:     answer.Answer,
			AnsweredBy: answer.AnsweredBy,
			AnsweredAt: answer.AnsweredAt,
		})
	case channelv1.RunnerPause:
		c.service.executions.Pause()

		return nil
	case channelv1.RunnerResume:
		c.service.executions.Resume()

		return nil
	case channelv1.RunnerConfigure:
		var configuration channelv1.Configuration

		if err := json.Unmarshal(message.Payload, &configuration); err != nil {
			return c.unreadable(ctx, message, err)
		}

		c.service.executions.Configure(configuration)

		return nil
	default:
		logging.From(ctx).InfoContext(
			ctx,
			"norn sent a message this machine cannot act on yet",
			slog.String("type", string(message.Type)),
			slog.String("message_id", message.ID),
		)

		return nil
	}
}

func (c *connection) unreadable(
	ctx context.Context,
	message channelv1.Message,
	err error,
) error {
	logging.From(ctx).WarnContext(
		ctx,
		"norn sent a message this machine could not read",
		slog.String("type", string(message.Type)),
		slog.String("message_id", message.ID),
		slog.String("error", err.Error()),
	)

	return nil
}

func (c *connection) writePump(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case envelope := <-c.writes:
			if err := c.conn.Write(ctx, envelope); err != nil {
				return err
			}
		}
	}
}

func (c *connection) spoolPump(ctx context.Context) error {
	pulse := time.NewTicker(c.service.cfg.Heartbeat)
	defer pulse.Stop()

	for {
		sent, err := c.drain(ctx)
		if err != nil {
			return err
		}

		if sent {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pulse.C:
			if err := c.beat(ctx); err != nil {
				return err
			}
		case <-time.After(drainPause):
		}
	}
}

func (c *connection) drain(ctx context.Context) (bool, error) {
	waiting, err := c.service.spool.Head(ctx, c.service.spooling.Batch)
	if err != nil {
		return false, err
	}

	for _, message := range waiting {
		if err := c.deliver(ctx, message); err != nil {
			return false, err
		}
	}

	return len(waiting) > 0, nil
}

func (c *connection) deliver(ctx context.Context, message channelv1.Message) error {
	answered := make(chan struct{})

	c.mu.Lock()
	c.inflight = message.ID
	c.settled = answered
	c.mu.Unlock()

	select {
	case c.writes <- channelv1.Frame(message):
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case <-answered:
	case <-ctx.Done():
		return ctx.Err()
	}

	return c.service.spool.Acknowledge(ctx, message.ID)
}

func (c *connection) beat(ctx context.Context) error {
	raw, err := json.Marshal(c.service.executions.Pulse(ctx))
	if err != nil {
		return err
	}

	message, err := channelv1.NewRunnerMessage(
		channelv1.RunnerHeartbeat, "", raw, c.service.now(),
	)
	if err != nil {
		return err
	}

	select {
	case c.writes <- channelv1.Frame(message):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *connection) acknowledge(ctx context.Context, id string) error {
	select {
	case c.writes <- channelv1.Acknowledgement(id, c.service.now()):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *connection) settle(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if id == "" || id != c.inflight || c.settled == nil {
		return
	}

	close(c.settled)

	c.inflight = ""
	c.settled = nil
}

func (c *connection) remember(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.service.now()

	if when, spent := c.seen[id]; spent && now.Sub(when) < channelv1.SeenTTL {
		return false
	}

	for held, when := range c.seen {
		if now.Sub(when) >= channelv1.SeenTTL {
			delete(c.seen, held)
		}
	}

	c.seen[id] = now

	return true
}
