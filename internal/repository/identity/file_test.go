package identity_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/config"
	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
	identityrepo "github.com/usenorn/runner/internal/repository/identity"
)

func newRepository(t *testing.T) (repository.Identity, *statedir.Dir) {
	t.Helper()

	dir, err := statedir.New(config.State{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	return identityrepo.New(dir), dir
}

func anIdentity() entity.Identity {
	return entity.Identity{
		RunnerID:    uuid.New(),
		WorkspaceID: uuid.New(),
		AgentID:     uuid.New(),
		AgentName:   "opsy",
		RunnerName:  "test-box",
		Server:      "https://norn.example",
		Store:       entity.StoreKeyring,
		EnrolledAt:  time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC),
	}
}

func TestAMachineWithNoIdentityFileIsNotEnrolled(t *testing.T) {
	repo, _ := newRepository(t)

	if _, err := repo.Load(context.Background()); !errors.Is(err, entity.ErrNotEnrolled) {
		t.Fatalf("a missing identity file returned %v, want it read as not enrolled", err)
	}
}

func TestAnIdentityComesBackExactlyAsItWasWritten(t *testing.T) {
	repo, _ := newRepository(t)
	written := anIdentity()

	if err := repo.Save(context.Background(), written); err != nil {
		t.Fatalf("save identity: %v", err)
	}

	read, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}

	if read != written {
		t.Fatalf("the identity read back as %+v, want %+v", read, written)
	}
}

func TestTheIdentityFileIsReadableOnlyByItsOwner(t *testing.T) {
	repo, dir := newRepository(t)

	if err := repo.Save(context.Background(), anIdentity()); err != nil {
		t.Fatalf("save identity: %v", err)
	}

	info, err := os.Stat(dir.Identity())
	if err != nil {
		t.Fatalf("stat identity: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("the identity file is %o, want 0600 so no other account can read it", mode)
	}
}

func TestRewritingTheIdentityLeavesItReadableAndStillOwnerOnly(t *testing.T) {
	repo, dir := newRepository(t)

	if err := repo.Save(context.Background(), anIdentity()); err != nil {
		t.Fatalf("save the first identity: %v", err)
	}

	if err := os.Chmod(dir.Identity(), 0o644); err != nil {
		t.Fatalf("loosen the identity file: %v", err)
	}

	second := anIdentity()

	if err := repo.Save(context.Background(), second); err != nil {
		t.Fatalf("save the second identity: %v", err)
	}

	info, err := os.Stat(dir.Identity())
	if err != nil {
		t.Fatalf("stat identity: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("a rewritten identity file is %o, want 0600 rather than the mode it replaced", mode)
	}

	read, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}

	if read.RunnerID != second.RunnerID {
		t.Fatalf("the rewrite left the previous identity in place")
	}
}

func TestAnIdentityFileThatCannotBeReadIsSaidSoRatherThanTakenAsAbsent(t *testing.T) {
	repo, dir := newRepository(t)

	if err := os.WriteFile(dir.Identity(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write a broken identity: %v", err)
	}

	if _, err := repo.Load(context.Background()); !errors.Is(err, entity.ErrIdentityMalformed) {
		t.Fatalf("a broken identity file returned %v, want it named as unreadable", err)
	}

	if err := os.WriteFile(dir.Identity(), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write an empty identity: %v", err)
	}

	if _, err := repo.Load(context.Background()); !errors.Is(err, entity.ErrIdentityMalformed) {
		t.Fatalf("an identity naming no runner returned %v, want it named as unreadable", err)
	}
}

func TestClearingAnIdentityIsSafeToRepeat(t *testing.T) {
	repo, dir := newRepository(t)

	if err := repo.Save(context.Background(), anIdentity()); err != nil {
		t.Fatalf("save identity: %v", err)
	}

	for range 2 {
		if err := repo.Clear(context.Background()); err != nil {
			t.Fatalf("clear identity: %v", err)
		}
	}

	if _, err := os.Stat(dir.Identity()); !os.IsNotExist(err) {
		t.Fatalf("the identity file survived being cleared")
	}
}

func TestWritingAnIdentityLeavesNoTemporaryFileBehind(t *testing.T) {
	repo, dir := newRepository(t)

	if err := repo.Save(context.Background(), anIdentity()); err != nil {
		t.Fatalf("save identity: %v", err)
	}

	entries, err := os.ReadDir(dir.Root())
	if err != nil {
		t.Fatalf("read the state directory: %v", err)
	}

	for _, each := range entries {
		if each.Name() != "identity.json" && !each.IsDir() {
			t.Fatalf("writing the identity left %q behind", each.Name())
		}
	}
}
