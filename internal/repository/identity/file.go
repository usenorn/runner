package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/usenorn/runner/internal/entity"
	"github.com/usenorn/runner/internal/pkg/statedir"
	"github.com/usenorn/runner/internal/repository"
)

type stored struct {
	RunnerID    uuid.UUID `json:"runnerId"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
	AgentID     uuid.UUID `json:"agentId"`
	AgentName   string    `json:"agentName,omitempty"`
	RunnerName  string    `json:"runnerName"`
	Server      string    `json:"server"`
	Store       string    `json:"store"`
	EnrolledAt  time.Time `json:"enrolledAt"`
}

type fileIdentity struct {
	path string
}

func New(dir *statedir.Dir) repository.Identity {
	return &fileIdentity{path: dir.Identity()}
}

func (r *fileIdentity) Load(_ context.Context) (entity.Identity, error) {
	raw, err := os.ReadFile(r.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return entity.Identity{}, entity.ErrNotEnrolled
		}

		return entity.Identity{}, fmt.Errorf("read identity %s: %w", r.path, err)
	}

	var held stored

	if err := json.Unmarshal(raw, &held); err != nil {
		return entity.Identity{}, fmt.Errorf("%w: %s", entity.ErrIdentityMalformed, r.path)
	}

	if held.RunnerID == uuid.Nil {
		return entity.Identity{}, fmt.Errorf("%w: %s", entity.ErrIdentityMalformed, r.path)
	}

	store := entity.Store(held.Store)
	if !store.Valid() {
		store = entity.StoreKeyring
	}

	return entity.Identity{
		RunnerID:    held.RunnerID,
		WorkspaceID: held.WorkspaceID,
		AgentID:     held.AgentID,
		AgentName:   held.AgentName,
		RunnerName:  held.RunnerName,
		Server:      held.Server,
		Store:       store,
		EnrolledAt:  held.EnrolledAt,
	}, nil
}

func (r *fileIdentity) Save(_ context.Context, identity entity.Identity) error {
	raw, err := json.MarshalIndent(stored{
		RunnerID:    identity.RunnerID,
		WorkspaceID: identity.WorkspaceID,
		AgentID:     identity.AgentID,
		AgentName:   identity.AgentName,
		RunnerName:  identity.RunnerName,
		Server:      identity.Server,
		Store:       string(identity.Store),
		EnrolledAt:  identity.EnrolledAt.UTC(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode identity: %w", err)
	}

	return statedir.WriteSecret(r.path, append(raw, '\n'))
}

func (r *fileIdentity) Clear(_ context.Context) error {
	if err := os.Remove(r.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove identity %s: %w", r.path, err)
	}

	return nil
}
