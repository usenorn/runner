package credential

import (
	"context"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/usenorn/runner/internal/entity"
)

func aKeyringStore(t *testing.T) *stores {
	t.Helper()

	keyring.MockInit()

	return &stores{
		backends: map[entity.Store]backend{
			entity.StoreKeyring: &keyringStore{account: t.TempDir()},
		},
	}
}

func TestCredentialsComeBackFromTheKeystoreUnchanged(t *testing.T) {
	repo := aKeyringStore(t)
	written := someCredentials(t)

	if err := repo.Save(context.Background(), entity.StoreKeyring, written); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	read, err := repo.Load(context.Background(), entity.StoreKeyring)
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}

	if !read.DeviceKey.Equal(written.DeviceKey) || read.RefreshToken != written.RefreshToken {
		t.Fatalf("the credentials came back changed, so this machine can no longer authenticate")
	}
}

func TestAKeystoreWithNothingInItReportsNoCredentials(t *testing.T) {
	repo := aKeyringStore(t)

	if _, err := repo.Load(context.Background(), entity.StoreKeyring); !errors.Is(err, entity.ErrCredentialsMissing) {
		t.Fatalf("an empty keystore returned %v, want it read as empty", err)
	}
}

func TestAMachineWithNoUsableKeystoreIsToldSoRatherThanFailingObscurely(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service is running"))

	repo := &stores{
		backends: map[entity.Store]backend{
			entity.StoreKeyring: &keyringStore{account: t.TempDir()},
		},
	}

	err := repo.Save(context.Background(), entity.StoreKeyring, someCredentials(t))
	if !errors.Is(err, entity.ErrKeystoreUnavailable) {
		t.Fatalf("a host with no keystore returned %v, want it named so --insecure-store can be offered", err)
	}
}

func TestClearingTakesTheCredentialsOutOfEveryStore(t *testing.T) {
	keyring.MockInit()

	repo := &stores{
		backends: map[entity.Store]backend{
			entity.StoreKeyring:   &keyringStore{account: t.TempDir()},
			entity.StoreEncrypted: newEncrypted(t.TempDir()+"/credentials.enc", thisMachine),
		},
	}

	for _, store := range entity.Stores() {
		if err := repo.Save(context.Background(), store, someCredentials(t)); err != nil {
			t.Fatalf("save into the %s store: %v", store, err)
		}
	}

	if err := repo.Clear(context.Background()); err != nil {
		t.Fatalf("clear credentials: %v", err)
	}

	for _, store := range entity.Stores() {
		if _, err := repo.Load(context.Background(), store); !errors.Is(err, entity.ErrCredentialsMissing) {
			t.Fatalf("credentials survived in the %s store after disconnect: %v", store, err)
		}
	}
}

func TestAHostWithNoKeystoreIsFoundOutBeforeAnythingIsWritten(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service is running"))

	repo := &stores{
		backends: map[entity.Store]backend{
			entity.StoreKeyring: &keyringStore{account: t.TempDir()},
		},
	}

	if err := repo.Usable(context.Background(), entity.StoreKeyring); !errors.Is(err, entity.ErrKeystoreUnavailable) {
		t.Fatalf("a host with no keystore reported its store usable: %v", err)
	}
}

func TestDisconnectingWorksOnAHostWhereOneStoreCannotBeReachedAtAll(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service is running"))

	path := t.TempDir() + "/credentials.enc"

	repo := &stores{
		backends: map[entity.Store]backend{
			entity.StoreKeyring:   &keyringStore{account: t.TempDir()},
			entity.StoreEncrypted: newEncrypted(path, thisMachine),
		},
	}

	if err := repo.Save(context.Background(), entity.StoreEncrypted, someCredentials(t)); err != nil {
		t.Fatalf("save into the encrypted store: %v", err)
	}

	if err := repo.Clear(context.Background()); err != nil {
		t.Fatalf(
			"clearing failed because an unreachable store could not be cleared, which would strand "+
				"a machine that never used it: %v",
			err,
		)
	}

	if _, err := repo.Load(context.Background(), entity.StoreEncrypted); !errors.Is(err, entity.ErrCredentialsMissing) {
		t.Fatalf("the credential that was really there survived: %v", err)
	}
}

func TestTheAgentTokenIsKeptApartFromThisMachinesOwnCredentials(t *testing.T) {
	keyring.MockInit()

	account := t.TempDir()
	repo := &stores{
		backends: map[entity.Store]backend{entity.StoreKeyring: &keyringStore{account: account}},
		agents:   map[entity.Store]backend{entity.StoreKeyring: &keyringStore{account: account + "#agent-token"}},
	}
	ctx := context.Background()

	if _, err := repo.LoadAgentToken(ctx, entity.StoreKeyring); !errors.Is(err, entity.ErrAgentTokenMissing) {
		t.Fatalf("an empty store gave back %v, want the token reported missing", err)
	}

	if err := repo.Save(ctx, entity.StoreKeyring, someCredentials(t)); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	if err := repo.SaveAgentToken(ctx, entity.StoreKeyring, "sk-ant-oat01-kept"); err != nil {
		t.Fatalf("save the agent token: %v", err)
	}

	token, err := repo.LoadAgentToken(ctx, entity.StoreKeyring)
	if err != nil || token != "sk-ant-oat01-kept" {
		t.Fatalf("the agent token came back as %q, %v", token, err)
	}

	if _, err := repo.Load(ctx, entity.StoreKeyring); err != nil {
		t.Fatalf(
			"keeping the agent token broke this machine's own credentials: %v. The two are "+
				"separate secrets and one must never overwrite the other",
			err,
		)
	}
}
