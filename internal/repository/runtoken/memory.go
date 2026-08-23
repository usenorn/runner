package runtoken

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/usenorn/runner/internal/repository"
)

const tokenBytes = 32

type memoryToken struct {
	mu   sync.RWMutex
	held map[string]string
}

func New() repository.RunToken {
	return &memoryToken{held: map[string]string{}}
}

func (r *memoryToken) Mint(_ context.Context, run string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if minted, held := r.held[run]; held {
		return minted, nil
	}

	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("mint a token for %s: %w", run, err)
	}

	minted := base64.RawURLEncoding.EncodeToString(raw)
	r.held[run] = minted

	return minted, nil
}

func (r *memoryToken) Allows(_ context.Context, run string, token string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	minted, held := r.held[run]
	if !held || token == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(minted), []byte(token)) == 1
}

func (r *memoryToken) Release(_ context.Context, run string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.held, run)
}
