package credential

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/usenorn/runner/internal/entity"
)

func aStore(t *testing.T, secret func() ([]byte, error)) *stores {
	t.Helper()

	return &stores{
		backends: map[entity.Store]backend{
			entity.StoreEncrypted: newEncrypted(filepath.Join(t.TempDir(), "credentials.enc"), secret),
		},
	}
}

func someCredentials(t *testing.T) entity.Credentials {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate a device key: %v", err)
	}

	return entity.Credentials{DeviceKey: private, RefreshToken: "nrr_abcdefghijklmnop"}
}

func thisMachine() ([]byte, error) {
	return []byte("11111111111111111111111111111111"), nil
}

func TestCredentialsComeBackFromTheEncryptedStoreUnchanged(t *testing.T) {
	repo := aStore(t, thisMachine)
	written := someCredentials(t)

	if err := repo.Save(context.Background(), entity.StoreEncrypted, written); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	read, err := repo.Load(context.Background(), entity.StoreEncrypted)
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}

	if !read.DeviceKey.Equal(written.DeviceKey) {
		t.Fatalf("the device key came back different, so nothing this machine signs will verify")
	}

	if read.RefreshToken != written.RefreshToken {
		t.Fatalf("the refresh token came back as %q, want %q", read.RefreshToken, written.RefreshToken)
	}
}

func TestTheEncryptedCredentialFileIsReadableOnlyByItsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")
	repo := &stores{
		backends: map[entity.Store]backend{
			entity.StoreEncrypted: newEncrypted(path, thisMachine),
		},
	}

	if err := repo.Save(context.Background(), entity.StoreEncrypted, someCredentials(t)); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the credential file: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("the credential file is %o, want 0600", mode)
	}
}

func TestNothingReadableIsLeftInTheEncryptedCredentialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")
	repo := &stores{
		backends: map[entity.Store]backend{
			entity.StoreEncrypted: newEncrypted(path, thisMachine),
		},
	}

	written := someCredentials(t)

	if err := repo.Save(context.Background(), entity.StoreEncrypted, written); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the credential file: %v", err)
	}

	if string(raw) == "" {
		t.Fatalf("the credential file is empty")
	}

	for _, secret := range [][]byte{[]byte(written.RefreshToken), written.DeviceKey.Seed()} {
		if bytes.Contains(raw, secret) {
			t.Fatalf("a secret is readable in the credential file as it sits on disk")
		}
	}
}

func TestCredentialsSealedOnAnotherMachineAreRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")

	mine := &stores{
		backends: map[entity.Store]backend{
			entity.StoreEncrypted: newEncrypted(path, thisMachine),
		},
	}

	if err := mine.Save(context.Background(), entity.StoreEncrypted, someCredentials(t)); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	theirs := &stores{
		backends: map[entity.Store]backend{
			entity.StoreEncrypted: newEncrypted(path, func() ([]byte, error) {
				return []byte("22222222222222222222222222222222"), nil
			}),
		},
	}

	_, err := theirs.Load(context.Background(), entity.StoreEncrypted)
	if !errors.Is(err, entity.ErrCredentialsMissing) {
		t.Fatalf("a file copied from another machine returned %v, want it refused", err)
	}
}

func TestAMachineWithNoMachineIdIsToldRatherThanGivenAKeyItInvented(t *testing.T) {
	repo := aStore(t, func() ([]byte, error) { return nil, entity.ErrMachineSecretMissing })

	err := repo.Save(context.Background(), entity.StoreEncrypted, someCredentials(t))
	if !errors.Is(err, entity.ErrMachineSecretMissing) {
		t.Fatalf("a machine with no machine id returned %v, want it refused outright", err)
	}
}

func TestAStoreThatHasNeverBeenWrittenReportsNoCredentials(t *testing.T) {
	repo := aStore(t, thisMachine)

	_, err := repo.Load(context.Background(), entity.StoreEncrypted)
	if !errors.Is(err, entity.ErrCredentialsMissing) {
		t.Fatalf("an unwritten store returned %v, want it read as empty", err)
	}
}

func TestClearingTheStoreTakesTheCredentialsWithIt(t *testing.T) {
	repo := aStore(t, thisMachine)

	if err := repo.Save(context.Background(), entity.StoreEncrypted, someCredentials(t)); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	if err := repo.Clear(context.Background()); err != nil {
		t.Fatalf("clear credentials: %v", err)
	}

	if _, err := repo.Load(context.Background(), entity.StoreEncrypted); !errors.Is(err, entity.ErrCredentialsMissing) {
		t.Fatalf("credentials survived being cleared: %v", err)
	}
}

func TestAStoreIsCalledUsableWhenItIsSimplyEmpty(t *testing.T) {
	repo := aStore(t, thisMachine)

	if err := repo.Usable(context.Background(), entity.StoreEncrypted); err != nil {
		t.Fatalf("an empty but working store reported itself unusable: %v", err)
	}
}

func TestAStoreWithNoMachineSecretIsNotUsable(t *testing.T) {
	repo := aStore(t, func() ([]byte, error) { return nil, entity.ErrMachineSecretMissing })

	if err := repo.Usable(context.Background(), entity.StoreEncrypted); !errors.Is(err, entity.ErrMachineSecretMissing) {
		t.Fatalf("a host with no machine id reported its store usable: %v", err)
	}
}
