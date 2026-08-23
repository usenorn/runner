package session

import (
	"context"
	"errors"
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

type sessionsService struct {
	dashboard   repository.Dashboard
	identities  repository.Identity
	credentials repository.Credential
	cfg         config.Session
	now         func() time.Time

	mu      sync.Mutex
	report  entity.SessionReport
	session entity.Session
	backoff time.Duration

	resume chan struct{}
}

func New(
	dashboard repository.Dashboard,
	identities repository.Identity,
	credentials repository.Credential,
	cfg config.Session,
) service.Sessions {
	return &sessionsService{
		dashboard:   dashboard,
		identities:  identities,
		credentials: credentials,
		cfg:         cfg,
		now:         func() time.Time { return time.Now().UTC() },
		report:      entity.SessionReport{State: entity.SessionUnenrolled},
		resume:      make(chan struct{}, 1),
	}
}

func (s *sessionsService) Run(ctx context.Context) {
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

func (s *sessionsService) Access(ctx context.Context) (string, error) {
	s.mu.Lock()
	session, report := s.session, s.report
	s.mu.Unlock()

	if session.Live(s.now()) {
		return session.AccessToken, nil
	}

	if report.State.Settled() {
		return "", failure(report)
	}

	identity, err := s.identities.Load(ctx)
	if err != nil {
		return "", err
	}

	if renewed := s.exchange(ctx, identity); renewed.State != entity.SessionLive {
		return "", failure(renewed)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.session.AccessToken, nil
}

func (s *sessionsService) Ticket(ctx context.Context) (string, error) {
	s.mu.Lock()
	report := s.report
	s.mu.Unlock()

	if report.State.Settled() {
		return "", failure(report)
	}

	identity, err := s.identities.Load(ctx)
	if err != nil {
		return "", err
	}

	if renewed := s.exchange(ctx, identity); renewed.State != entity.SessionLive {
		return "", failure(renewed)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.session.TicketLive(s.now()) {
		return "", entity.ErrTicketMissing
	}

	return s.session.Ticket, nil
}

func (s *sessionsService) TunnelTicket(ctx context.Context) (entity.TunnelTicket, error) {
	s.mu.Lock()
	report := s.report
	s.mu.Unlock()

	if report.State.Settled() {
		return entity.TunnelTicket{}, failure(report)
	}

	identity, err := s.identities.Load(ctx)
	if err != nil {
		return entity.TunnelTicket{}, err
	}

	if renewed := s.exchange(ctx, identity); renewed.State != entity.SessionLive {
		return entity.TunnelTicket{}, failure(renewed)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.session.Previews.Serving() {
		return entity.TunnelTicket{}, entity.ErrPreviewsUnserved
	}

	if !s.session.TunnelLive(s.now()) {
		return entity.TunnelTicket{}, entity.ErrTicketMissing
	}

	return entity.TunnelTicket{
		Ticket:   s.session.TunnelTicket,
		Previews: s.session.Previews,
	}, nil
}

func (s *sessionsService) Previews() entity.PreviewService {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.session.Previews
}

func failure(report entity.SessionReport) error {
	switch report.State {
	case entity.SessionRevoked:
		return entity.ErrRunnerRevoked
	case entity.SessionCredentialInvalid:
		return entity.ErrCredentialInvalid
	case entity.SessionClockSkew:
		return entity.ErrClockSkew
	case entity.SessionUnenrolled:
		return entity.ErrNotEnrolled
	default:
		return entity.UnreachableError{Detail: report.Detail}
	}
}

func (s *sessionsService) Report() entity.SessionReport {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.report
}

func (s *sessionsService) Adopt(ctx context.Context, identity entity.Identity) entity.SessionReport {
	s.mu.Lock()
	s.session = entity.Session{}
	s.backoff = 0
	s.report = entity.SessionReport{State: entity.SessionConnecting}
	s.mu.Unlock()

	report := s.exchange(ctx, identity)

	s.wake()

	return report
}

func (s *sessionsService) Forget() {
	s.mu.Lock()
	s.session = entity.Session{}
	s.backoff = 0
	s.report = entity.SessionReport{State: entity.SessionUnenrolled}
	s.mu.Unlock()

	s.wake()
}

func (s *sessionsService) tick(ctx context.Context) time.Duration {
	identity, err := s.identities.Load(ctx)
	if err != nil {
		if errors.Is(err, entity.ErrNotEnrolled) {
			s.settle(entity.SessionReport{State: entity.SessionUnenrolled})

			return idleWait
		}

		return s.stall(entity.SessionReport{State: entity.SessionOffline, Detail: err.Error()})
	}

	s.mu.Lock()
	settled := s.report.State.Settled()
	live := s.session.Live(s.now().Add(s.cfg.RefreshLead))
	session := s.session
	s.mu.Unlock()

	if settled {
		return idleWait
	}

	if live {
		return session.RefreshIn(s.now(), s.cfg.RefreshLead)
	}

	report := s.exchange(ctx, identity)

	switch report.State {
	case entity.SessionLive:
		s.mu.Lock()
		wait := s.session.RefreshIn(s.now(), s.cfg.RefreshLead)
		s.mu.Unlock()

		return wait
	case entity.SessionRevoked, entity.SessionCredentialInvalid:
		return idleWait
	default:
		return s.nextBackoff()
	}
}

func (s *sessionsService) exchange(ctx context.Context, identity entity.Identity) entity.SessionReport {
	credentials, err := s.credentials.Load(ctx, identity.Store)
	if err != nil {
		if errors.Is(err, entity.ErrCredentialsMissing) {
			return s.settle(entity.SessionReport{
				State:  entity.SessionCredentialInvalid,
				Detail: err.Error(),
			})
		}

		return s.settle(entity.SessionReport{State: entity.SessionOffline, Detail: err.Error()})
	}

	assertion, err := entity.NewAssertion(identity.RunnerID, s.now())
	if err != nil {
		return s.settle(entity.SessionReport{State: entity.SessionOffline, Detail: err.Error()})
	}

	session, err := s.dashboard.Exchange(
		ctx, credentials.RefreshToken, assertion, assertion.Sign(credentials.DeviceKey),
	)
	if err != nil {
		logging.From(ctx).WarnContext(
			ctx, "the runner could not renew its session", slog.String("error", err.Error()),
		)

		return s.settle(entity.SessionReport{State: stateFor(err), Detail: err.Error()})
	}

	s.rename(ctx, identity, session)

	s.mu.Lock()
	s.session = session
	s.backoff = 0
	s.report = entity.SessionReport{State: entity.SessionLive, ExpiresAt: session.AccessExpiresAt}
	report := s.report
	s.mu.Unlock()

	return report
}

func (s *sessionsService) rename(ctx context.Context, identity entity.Identity, session entity.Session) {
	renamed := false

	if session.AgentName != "" && session.AgentName != identity.AgentName {
		identity.AgentName = session.AgentName
		renamed = true
	}

	if session.RunnerName != "" && session.RunnerName != identity.RunnerName {
		identity.RunnerName = session.RunnerName
		renamed = true
	}

	if !renamed {
		return
	}

	if err := s.identities.Save(ctx, identity); err != nil {
		logging.From(ctx).WarnContext(
			ctx,
			"the runner could not record what norn now calls it",
			slog.String("error", err.Error()),
		)
	}
}

func (s *sessionsService) settle(report entity.SessionReport) entity.SessionReport {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.report = report
	s.session = entity.Session{}

	return report
}

func (s *sessionsService) stall(report entity.SessionReport) time.Duration {
	s.settle(report)

	return s.nextBackoff()
}

func (s *sessionsService) nextBackoff() time.Duration {
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

func (s *sessionsService) wake() {
	select {
	case s.resume <- struct{}{}:
	default:
	}
}

func stateFor(err error) entity.SessionState {
	switch {
	case errors.Is(err, entity.ErrRunnerRevoked):
		return entity.SessionRevoked
	case errors.Is(err, entity.ErrCredentialInvalid), errors.Is(err, entity.ErrAssertionRefused):
		return entity.SessionCredentialInvalid
	case errors.Is(err, entity.ErrClockSkew):
		return entity.SessionClockSkew
	default:
		return entity.SessionOffline
	}
}
