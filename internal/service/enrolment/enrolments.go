package enrolment

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/repository"
	"github.com/usenorn/runner/internal/service"
)

type enrolmentsService struct {
	dashboard   repository.Dashboard
	identities  repository.Identity
	credentials repository.Credential
	sessions    service.Sessions
	host        entity.Host
}

func New(
	dashboard repository.Dashboard,
	identities repository.Identity,
	credentials repository.Credential,
	sessions service.Sessions,
	host entity.Host,
) service.Enrolments {
	return &enrolmentsService{
		dashboard:   dashboard,
		identities:  identities,
		credentials: credentials,
		sessions:    sessions,
		host:        host,
	}
}

func (s *enrolmentsService) Current(ctx context.Context) (entity.Identity, error) {
	return s.identities.Load(ctx)
}

func (s *enrolmentsService) Connect(
	ctx context.Context,
	input service.ConnectInput,
) (service.Connected, error) {
	if err := s.replaceable(ctx, input.Force); err != nil {
		return service.Connected{}, err
	}

	store := input.Store
	if !store.Valid() {
		store = entity.StoreKeyring
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = s.host.Hostname
	}

	if err := entity.ValidateEnrolment(name, s.host); err != nil {
		return service.Connected{}, err
	}

	if err := s.credentials.Usable(ctx, store); err != nil {
		return service.Connected{}, err
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return service.Connected{}, fmt.Errorf("generate this machine's device key: %w", err)
	}

	enrolled, err := s.dashboard.Enrol(ctx, input.Token, repository.Enrolment{
		Name:      name,
		Host:      s.host,
		PublicKey: public,
	})
	if err != nil {
		return service.Connected{}, err
	}

	if err := s.forget(ctx); err != nil {
		return service.Connected{}, err
	}

	credentials := entity.Credentials{DeviceKey: private, RefreshToken: enrolled.RefreshToken}

	if err := s.credentials.Save(ctx, store, credentials); err != nil {
		return service.Connected{}, orphaned(enrolled.Identity, err)
	}

	identity := enrolled.Identity
	identity.Store = store

	if err := s.identities.Save(ctx, identity); err != nil {
		return service.Connected{}, orphaned(identity, err)
	}

	return service.Connected{Identity: identity, Session: s.sessions.Adopt(ctx, identity)}, nil
}

func (s *enrolmentsService) Disconnect(ctx context.Context) (entity.Identity, error) {
	identity, err := s.identities.Load(ctx)
	if err != nil {
		return entity.Identity{}, err
	}

	if err := s.forget(ctx); err != nil {
		return entity.Identity{}, err
	}

	return identity, nil
}

func (s *enrolmentsService) forget(ctx context.Context) error {
	s.sessions.Forget()

	if err := s.credentials.Clear(ctx); err != nil {
		return err
	}

	return s.identities.Clear(ctx)
}

func (s *enrolmentsService) replaceable(ctx context.Context, force bool) error {
	_, err := s.identities.Load(ctx)

	switch {
	case errors.Is(err, entity.ErrNotEnrolled):
		return nil
	case err != nil && !errors.Is(err, entity.ErrIdentityMalformed):
		return err
	case !force:
		return entity.ErrAlreadyEnrolled
	default:
		return nil
	}
}

func orphaned(identity entity.Identity, err error) error {
	return fmt.Errorf(
		"%w: %q is now on record in Norn but can never authenticate, because %s. Revoke %q under "+
			"settings, runners, then connect again",
		entity.ErrEnrolmentStranded, identity.RunnerName, err, identity.RunnerName,
	)
}
