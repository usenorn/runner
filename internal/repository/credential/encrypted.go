package credential

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
)

const (
	secretKeyLen = 32
	secretInfo   = "norn-runner credential store"
)

var machineIDFiles = []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}

type encryptedStore struct {
	path   string
	secret func() ([]byte, error)
}

func newEncrypted(path string, secret func() ([]byte, error)) backend {
	return &encryptedStore{path: path, secret: secret}
}

func (b *encryptedStore) usable(_ context.Context) error {
	_, err := b.sealer()

	return err
}

func (b *encryptedStore) read(_ context.Context) ([]byte, error) {
	stored, err := os.ReadFile(b.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, entity.ErrCredentialsMissing
		}

		return nil, fmt.Errorf("read credentials %s: %w", b.path, err)
	}

	sealer, err := b.sealer()
	if err != nil {
		return nil, err
	}

	if len(stored) < sealer.NonceSize() {
		return nil, entity.ErrCredentialsMissing
	}

	raw, err := sealer.Open(nil, stored[:sealer.NonceSize()], stored[sealer.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %s was sealed on another machine or with another key", entity.ErrCredentialsMissing, b.path,
		)
	}

	return raw, nil
}

func (b *encryptedStore) write(_ context.Context, raw []byte) error {
	sealer, err := b.sealer()
	if err != nil {
		return err
	}

	nonce := make([]byte, sealer.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate a credential nonce: %w", err)
	}

	return statedir.WriteSecret(b.path, sealer.Seal(nonce, nonce, raw, nil))
}

func (b *encryptedStore) remove(_ context.Context) error {
	if err := os.Remove(b.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove credentials %s: %w", b.path, err)
	}

	return nil
}

func (b *encryptedStore) sealer() (cipher.AEAD, error) {
	secret, err := b.secret()
	if err != nil {
		return nil, err
	}

	key, err := hkdf.Key(sha256.New, secret, []byte(b.path), secretInfo, secretKeyLen)
	if err != nil {
		return nil, fmt.Errorf("derive the credential key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build the credential cipher: %w", err)
	}

	sealer, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build the credential sealer: %w", err)
	}

	return sealer, nil
}

func machineSecret() ([]byte, error) {
	for _, path := range machineIDFiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		if trimmed := strings.TrimSpace(string(raw)); trimmed != "" {
			return []byte(trimmed), nil
		}
	}

	return nil, entity.ErrMachineSecretMissing
}
