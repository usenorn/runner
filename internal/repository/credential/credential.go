package credential

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
)

type sealed struct {
	DeviceKeySeed string `json:"deviceKeySeed"`
	RefreshToken  string `json:"refreshToken"`
}

type backend interface {
	usable(ctx context.Context) error
	read(ctx context.Context) ([]byte, error)
	write(ctx context.Context, raw []byte) error
	remove(ctx context.Context) error
}

type stores struct {
	backends map[entity.Store]backend
}

func New(dir *statedir.Dir) repository.Credential {
	return &stores{
		backends: map[entity.Store]backend{
			entity.StoreKeyring:   newKeyring(dir),
			entity.StoreEncrypted: newEncrypted(dir.Credentials(), machineSecret),
		},
	}
}

func (r *stores) Usable(ctx context.Context, store entity.Store) error {
	chosen, err := r.backend(store)
	if err != nil {
		return err
	}

	return chosen.usable(ctx)
}

func (r *stores) Load(ctx context.Context, store entity.Store) (entity.Credentials, error) {
	chosen, err := r.backend(store)
	if err != nil {
		return entity.Credentials{}, err
	}

	raw, err := chosen.read(ctx)
	if err != nil {
		return entity.Credentials{}, err
	}

	var held sealed

	if err := json.Unmarshal(raw, &held); err != nil {
		return entity.Credentials{}, entity.ErrCredentialsMissing
	}

	seed, err := base64.StdEncoding.DecodeString(held.DeviceKeySeed)
	if err != nil || len(seed) != ed25519.SeedSize || held.RefreshToken == "" {
		return entity.Credentials{}, entity.ErrCredentialsMissing
	}

	return entity.Credentials{
		DeviceKey:    ed25519.NewKeyFromSeed(seed),
		RefreshToken: held.RefreshToken,
	}, nil
}

func (r *stores) Save(ctx context.Context, store entity.Store, credentials entity.Credentials) error {
	chosen, err := r.backend(store)
	if err != nil {
		return err
	}

	if !credentials.Complete() {
		return entity.ErrCredentialsMissing
	}

	raw, err := json.Marshal(sealed{
		DeviceKeySeed: base64.StdEncoding.EncodeToString(credentials.DeviceKey.Seed()),
		RefreshToken:  credentials.RefreshToken,
	})
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}

	return chosen.write(ctx, raw)
}

func (r *stores) Clear(ctx context.Context) error {
	var failures []error

	for _, chosen := range r.backends {
		err := chosen.remove(ctx)

		switch {
		case err == nil,
			errors.Is(err, entity.ErrCredentialsMissing),
			errors.Is(err, entity.ErrKeystoreUnavailable),
			errors.Is(err, entity.ErrMachineSecretMissing):
		default:
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

func (r *stores) backend(store entity.Store) (backend, error) {
	chosen, ok := r.backends[store]
	if !ok {
		return nil, fmt.Errorf("%w: %q", entity.ErrCredentialsMissing, store)
	}

	return chosen, nil
}
