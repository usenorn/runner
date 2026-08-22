package channel

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

const idleWait = time.Hour

type channelsService struct {
	channels   repository.Channel
	spool      repository.Spool
	sessions   service.Sessions
	executions service.Executions
	cfg        config.Channel
	spooling   config.Spool
	app        config.App
	now        func() time.Time

	mu      sync.Mutex
	report  entity.ChannelReport
	backoff time.Duration

	resume chan struct{}
}

func New(
	channels repository.Channel,
	spool repository.Spool,
	sessions service.Sessions,
	executions service.Executions,
	cfg config.Channel,
	spooling config.Spool,
	app config.App,
) service.Channels {
	return &channelsService{
		channels:   channels,
		spool:      spool,
		sessions:   sessions,
		executions: executions,
		cfg:        cfg,
		spooling:   spooling,
		app:        app,
		now:        func() time.Time { return time.Now().UTC() },
		report:     entity.ChannelReport{State: entity.ChannelOff},
		resume:     make(chan struct{}, 1),
	}
}

func (s *channelsService) Run(ctx context.Context) {
	if !s.cfg.Enabled {
		s.settle(entity.ChannelReport{
			State:  entity.ChannelOff,
			Detail: "the channel is switched off in this machine's configuration",
		})

		<-ctx.Done()

		return
	}

	if err := s.executions.Recover(ctx); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not read back the runs it was holding",
			slog.String("error", err.Error()),
		)
	}

	for {
		wait := s.tick(ctx)

		timer := time.NewTimer(wait)

		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-timer.C:
		case <-s.resume:
			timer.Stop()
		}
	}
}

func (s *channelsService) Report(ctx context.Context) entity.ChannelReport {
	s.mu.Lock()
	report := s.report
	s.mu.Unlock()

	waiting, err := s.spool.Count(ctx)
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not count what is waiting to reach norn",
			slog.String("error", err.Error()),
		)

		return report
	}

	report.Waiting = waiting

	return report
}

func (s *channelsService) Wake() {
	select {
	case s.resume <- struct{}{}:
	default:
	}
}

func (s *channelsService) tick(ctx context.Context) time.Duration {
	s.mu.Lock()
	settled := s.report.State.Settled()
	s.mu.Unlock()

	if settled {
		return idleWait
	}

	if err := s.prune(ctx); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine could not tidy what is waiting to reach norn",
			slog.String("error", err.Error()),
		)
	}

	s.settle(entity.ChannelReport{State: entity.ChannelConnecting})

	err := s.connect(ctx)

	if ctx.Err() != nil {
		return idleWait
	}

	state := entity.ChannelStateFor(err)

	s.settle(entity.ChannelReport{State: state, Detail: detailOf(err)})

	if state.Settled() {
		logging.From(ctx).ErrorContext(
			ctx,
			"norn will not open a channel to this machine",
			slog.String("error", err.Error()),
		)

		return idleWait
	}

	logging.From(ctx).InfoContext(
		ctx, "the channel to norn ended", slog.String("error", detailOf(err)),
	)

	return s.nextBackoff()
}

func (s *channelsService) connect(ctx context.Context) error {
	ticket, err := s.sessions.Ticket(ctx)
	if err != nil {
		return err
	}

	conn, err := s.channels.Dial(ctx, ticket, s.app.Version)
	if err != nil {
		return err
	}

	defer func() { _ = conn.Close() }()

	s.mu.Lock()
	s.backoff = 0
	s.report = entity.ChannelReport{State: entity.ChannelLive, ConnectedAt: s.now()}
	s.mu.Unlock()

	logging.From(ctx).InfoContext(ctx, "this machine is connected to norn")

	held := &connection{
		service: s,
		conn:    conn,
		writes:  make(chan channelv1.Envelope, outbound),
		seen:    map[string]time.Time{},
	}

	return held.run(ctx)
}

func (s *channelsService) prune(ctx context.Context) error {
	dropped, err := s.spool.Prune(
		ctx, s.now().Add(-s.spooling.MaxAge), s.spooling.MaxMessages,
	)
	if err != nil {
		return err
	}

	if dropped > 0 {
		logging.From(ctx).WarnContext(
			ctx,
			"events waited for norn longer than this machine keeps them and were dropped",
			slog.Int("dropped", dropped),
		)
	}

	return nil
}

func (s *channelsService) settle(report entity.ChannelReport) {
	s.mu.Lock()
	defer s.mu.Unlock()

	report.LastHeard = s.report.LastHeard

	if report.State == entity.ChannelLive {
		report.ConnectedAt = s.report.ConnectedAt
	}

	s.report = report
}

func (s *channelsService) heard() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.report.LastHeard = s.now()
}

func (s *channelsService) nextBackoff() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.backoff == 0 {
		s.backoff = s.cfg.RetryMin
	} else {
		s.backoff = min(s.backoff*2, s.cfg.RetryMax)
	}

	spread := int64(s.backoff / 4)
	if spread < 1 {
		return s.backoff
	}

	return s.backoff - time.Duration(rand.Int64N(spread))
}

func detailOf(err error) string {
	if err == nil || errors.Is(err, context.Canceled) {
		return ""
	}

	return err.Error()
}
