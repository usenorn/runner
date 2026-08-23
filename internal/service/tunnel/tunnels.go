package tunnel

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/observability/logging"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

const idleWait = time.Hour

type tunnelsService struct {
	tunnels  repository.Tunnel
	sessions service.Sessions
	previews service.Previews
	cfg      config.Tunnel
	now      func() time.Time

	mu      sync.Mutex
	report  entity.TunnelReport
	streams int
	backoff time.Duration

	resume chan struct{}
}

func New(
	tunnels repository.Tunnel,
	sessions service.Sessions,
	previews service.Previews,
	cfg config.Tunnel,
) service.Tunnels {
	return &tunnelsService{
		tunnels:  tunnels,
		sessions: sessions,
		previews: previews,
		cfg:      cfg,
		now:      func() time.Time { return time.Now().UTC() },
		report:   entity.TunnelReport{State: entity.TunnelOff},
		resume:   make(chan struct{}, 1),
	}
}

func (s *tunnelsService) Run(ctx context.Context) {
	if !s.cfg.Enabled {
		s.settle(entity.TunnelReport{State: entity.TunnelOff})

		<-ctx.Done()

		return
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

func (s *tunnelsService) Wake() {
	select {
	case s.resume <- struct{}{}:
	default:
	}
}

func (s *tunnelsService) Report() entity.TunnelReport {
	s.mu.Lock()
	defer s.mu.Unlock()

	held := s.report
	held.Streams = s.streams

	return held
}

func (s *tunnelsService) tick(ctx context.Context) time.Duration {
	if s.Report().State.Settled() {
		return idleWait
	}

	s.settle(entity.TunnelReport{State: entity.TunnelConnecting})

	err := s.connect(ctx)
	if err == nil || ctx.Err() != nil {
		return s.cfg.RetryMin
	}

	state := entity.TunnelStateFor(err)

	s.settle(entity.TunnelReport{State: state, Detail: err.Error()})

	if state.Settled() {
		logging.From(ctx).WarnContext(
			ctx,
			"this machine will not open a preview tunnel again until it is restarted",
			slog.String("state", string(state)),
			slog.String("reason", err.Error()),
		)

		return idleWait
	}

	return s.nextBackoff()
}

func (s *tunnelsService) connect(ctx context.Context) error {
	ticket, err := s.sessions.TunnelTicket(ctx)
	if err != nil {
		return err
	}

	session, err := s.tunnels.Dial(ctx, ticket)
	if err != nil {
		return err
	}

	defer func() { _ = session.Close() }()

	s.mu.Lock()
	s.backoff = s.cfg.RetryMin
	s.mu.Unlock()

	s.settle(entity.TunnelReport{
		State:       entity.TunnelLive,
		Gateway:     ticket.Previews.Gateway,
		ConnectedAt: s.now(),
	})

	logging.From(ctx).InfoContext(
		ctx,
		"this machine is carrying previews through norn's gateway",
		slog.String("gateway", ticket.Previews.Gateway),
	)

	return s.carry(ctx, session)
}

func (s *tunnelsService) carry(ctx context.Context, session repository.TunnelSession) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	go func() {
		<-ctx.Done()

		_ = session.Close()
	}()

	for {
		stream, err := session.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			return err
		}

		go s.serve(ctx, stream)
	}
}

func (s *tunnelsService) settle(report entity.TunnelReport) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.report = report
}

func (s *tunnelsService) nextBackoff() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.backoff <= 0 {
		s.backoff = s.cfg.RetryMin
	}

	waiting := s.backoff

	s.backoff *= 2
	if s.backoff > s.cfg.RetryMax {
		s.backoff = s.cfg.RetryMax
	}

	spread := waiting / 4
	if spread <= 0 {
		return waiting
	}

	return waiting - time.Duration(rand.Int64N(int64(spread)))
}
