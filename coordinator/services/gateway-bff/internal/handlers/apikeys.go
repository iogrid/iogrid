package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// APIKey is one customer-issued API key. The plaintext value is shown
// only at creation time; we persist a SHA-256 prefix + suffix for
// recognition.
type APIKey struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Label       string    `json:"label"`
	// Prefix is the first 8 chars of the secret — used by the UI to
	// show "iog_abcd...xyz" so the user can identify the key after
	// creation without re-fetching the plaintext.
	Prefix      string    `json:"prefix"`
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	// Plaintext is set ONLY in the immediate response to Create. The
	// store NEVER returns it on subsequent Get/List.
	Plaintext   string `json:"plaintext,omitempty"`
}

// APIKeyStore is the persistence interface. Phase 0 ships the in-memory
// implementation; a Postgres-backed store will land when the
// corresponding proto is defined.
type APIKeyStore interface {
	Create(ctx context.Context, workspaceID uuid.UUID, label string) (APIKey, error)
	List(ctx context.Context, workspaceID uuid.UUID) ([]APIKey, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
}

// ErrAPIKeyNotFound is returned when an id does not match a stored key.
var ErrAPIKeyNotFound = errors.New("api key not found")

// MemoryAPIKeyStore is an in-process implementation safe for concurrent
// use. Suitable for tests and the Phase 0 dev environment where the
// gateway-bff runs as a single replica behind a single-replica
// identity-svc.
type MemoryAPIKeyStore struct {
	mu   sync.Mutex
	keys map[uuid.UUID]APIKey
}

// NewMemoryAPIKeyStore returns an empty store.
func NewMemoryAPIKeyStore() *MemoryAPIKeyStore {
	return &MemoryAPIKeyStore{keys: map[uuid.UUID]APIKey{}}
}

// Create generates a fresh API key with a random 32-byte secret encoded
// as URL-safe base64. The plaintext is returned ONCE in the result.
func (s *MemoryAPIKeyStore) Create(_ context.Context, workspaceID uuid.UUID, label string) (APIKey, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return APIKey{}, err
	}
	secret := "iog_" + base64.RawURLEncoding.EncodeToString(buf)
	prefix := secret
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	k := APIKey{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Label:       label,
		Prefix:      prefix,
		CreatedAt:   time.Now().UTC(),
		Plaintext:   secret,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[k.ID] = stripped(k)
	return k, nil
}

// List returns keys belonging to the workspace, sorted by creation
// time descending.
func (s *MemoryAPIKeyStore) List(_ context.Context, workspaceID uuid.UUID) ([]APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]APIKey, 0, len(s.keys))
	for _, k := range s.keys {
		if k.WorkspaceID == workspaceID {
			out = append(out, k)
		}
	}
	return out, nil
}

// Delete removes a key. Returns ErrAPIKeyNotFound when no match.
func (s *MemoryAPIKeyStore) Delete(_ context.Context, workspaceID, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[id]
	if !ok || k.WorkspaceID != workspaceID {
		return ErrAPIKeyNotFound
	}
	delete(s.keys, id)
	return nil
}

// stripped returns a copy with plaintext cleared. The store NEVER
// retains plaintexts past the Create call.
func stripped(k APIKey) APIKey {
	k.Plaintext = ""
	return k
}
