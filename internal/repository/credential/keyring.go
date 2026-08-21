package credential

import (
	"context"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

const keyringService = "site.norn.runner"

type keyringStore struct {
	account string
}

func newKeyring(dir *statedir.Dir) backend {
	return &keyringStore{account: dir.Root()}
}

func (b *keyringStore) usable(ctx context.Context) error {
	if _, err := b.read(ctx); err != nil && !errors.Is(err, entity.ErrCredentialsMissing) {
		return err
	}

	return nil
}

func (b *keyringStore) read(_ context.Context) ([]byte, error) {
	raw, err := keyring.Get(keyringService, b.account)
	if err != nil {
		return nil, b.translate(err)
	}

	return []byte(raw), nil
}

func (b *keyringStore) write(_ context.Context, raw []byte) error {
	if err := keyring.Set(keyringService, b.account, string(raw)); err != nil {
		return b.translate(err)
	}

	return nil
}

func (b *keyringStore) remove(_ context.Context) error {
	if err := keyring.Delete(keyringService, b.account); err != nil {
		return b.translate(err)
	}

	return nil
}

func (b *keyringStore) translate(err error) error {
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return entity.ErrCredentialsMissing
	case errors.Is(err, keyring.ErrUnsupportedPlatform):
		return entity.ErrKeystoreUnavailable
	default:
		return fmt.Errorf("%w: %s", entity.ErrKeystoreUnavailable, err)
	}
}
