package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisStateStore persists pendingState in Redis under a namespaced key
// with a TTL. Pop is implemented as GET-then-DEL (no atomic GETDEL on
// older Redis versions; we accept the racey window because state tokens
// are one-shot and double-use ends in a Google nonce mismatch anyway).
type redisStateStore struct {
	r *redis.Client
}

const redisStatePrefix = "iogrid:identity:oauth_state:"

func (s *redisStateStore) put(ctx context.Context, key string, val pendingState, ttl time.Duration) error {
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return s.r.Set(ctx, redisStatePrefix+key, b, ttl).Err()
}

func (s *redisStateStore) pop(ctx context.Context, key string) (pendingState, error) {
	full := redisStatePrefix + key
	val, err := s.r.Get(ctx, full).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return pendingState{}, fmt.Errorf("state not found")
		}
		return pendingState{}, err
	}
	_ = s.r.Del(ctx, full).Err()
	var out pendingState
	if err := json.Unmarshal(val, &out); err != nil {
		return pendingState{}, err
	}
	return out, nil
}

// memoryStateStore is the in-memory fallback used when Redis is not
// configured (unit tests / `go run` dev mode). It is process-local and
// must not be relied on across replicas.
type memoryStateStore struct {
	mu sync.Mutex
	m  map[string]memEntry
}

type memEntry struct {
	val     pendingState
	expires time.Time
}

func newMemoryStateStore() *memoryStateStore {
	return &memoryStateStore{m: make(map[string]memEntry)}
}

func (s *memoryStateStore) put(_ context.Context, key string, val pendingState, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = memEntry{val: val, expires: time.Now().Add(ttl)}
	return nil
}

func (s *memoryStateStore) pop(_ context.Context, key string) (pendingState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok {
		return pendingState{}, fmt.Errorf("state not found")
	}
	delete(s.m, key)
	if time.Now().After(e.expires) {
		return pendingState{}, fmt.Errorf("state expired")
	}
	return e.val, nil
}
