// Package sessions implements the sticky-session ledger.
//
// Per docs/ARCHITECTURE.md, the same (customer_id, destination) pair
// MUST route to the same provider for up to 30 minutes (configurable
// via SESSION_TTL), so that customer-side scrapers don't appear to
// flip IPs mid-walk and trigger destination-side anti-bot heuristics.
//
// The ledger is best-effort: Redis is the production backend, but the
// proxy MUST keep working when Redis is unreachable (sessions become
// non-sticky, never lock the data plane). When REDIS_URL is unset the
// in-memory Map backend is used.
package sessions

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrNotFound is returned by Get when no sticky binding exists yet.
var ErrNotFound = errors.New("session not found")

// Binding records the sticky provider for a (customer, destination) pair.
type Binding struct {
	CustomerID  string
	Destination string
	ProviderID  string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Store is the sticky-session ledger contract.
type Store interface {
	// Get returns the live binding, or ErrNotFound.
	Get(ctx context.Context, customerID, destination string) (*Binding, error)
	// Put records (or refreshes) a binding.
	Put(ctx context.Context, b Binding) error
	// Invalidate removes a binding (called when the bound provider
	// drops or returns an error).
	Invalidate(ctx context.Context, customerID, destination string) error
}

// Memory is the in-memory Store implementation. Safe for concurrent use.
type Memory struct {
	mu  sync.RWMutex
	now func() time.Time
	m   map[string]Binding
	ttl time.Duration
}

// NewMemory constructs an in-memory store with the supplied TTL.
func NewMemory(ttl time.Duration) *Memory {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &Memory{
		now: time.Now,
		m:   map[string]Binding{},
		ttl: ttl,
	}
}

func (m *Memory) key(customerID, destination string) string {
	return strings.ToLower(customerID) + "|" + strings.ToLower(destination)
}

// Get implements Store.
func (m *Memory) Get(_ context.Context, customerID, destination string) (*Binding, error) {
	m.mu.RLock()
	b, ok := m.m[m.key(customerID, destination)]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	if !b.ExpiresAt.IsZero() && m.now().After(b.ExpiresAt) {
		m.mu.Lock()
		delete(m.m, m.key(customerID, destination))
		m.mu.Unlock()
		return nil, ErrNotFound
	}
	return &b, nil
}

// Put implements Store.
func (m *Memory) Put(_ context.Context, b Binding) error {
	if b.ExpiresAt.IsZero() {
		b.ExpiresAt = m.now().Add(m.ttl)
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = m.now()
	}
	m.mu.Lock()
	m.m[m.key(b.CustomerID, b.Destination)] = b
	m.mu.Unlock()
	return nil
}

// Invalidate implements Store.
func (m *Memory) Invalidate(_ context.Context, customerID, destination string) error {
	m.mu.Lock()
	delete(m.m, m.key(customerID, destination))
	m.mu.Unlock()
	return nil
}

// Redis is the Redis-backed Store implementation.
type Redis struct {
	rdb *redis.Client
	ttl time.Duration
	now func() time.Time
}

// NewRedis returns a Redis-backed Store.
func NewRedis(rdb *redis.Client, ttl time.Duration) *Redis {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &Redis{rdb: rdb, ttl: ttl, now: time.Now}
}

func redisKey(customerID, destination string) string {
	return "iogrid:proxy:sticky:" + strings.ToLower(customerID) + ":" + strings.ToLower(destination)
}

// Get implements Store.
func (r *Redis) Get(ctx context.Context, customerID, destination string) (*Binding, error) {
	res, err := r.rdb.HGetAll(ctx, redisKey(customerID, destination)).Result()
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, ErrNotFound
	}
	b := &Binding{
		CustomerID:  res["customer_id"],
		Destination: res["destination"],
		ProviderID:  res["provider_id"],
	}
	if v, ok := res["created_at"]; ok {
		if t, perr := time.Parse(time.RFC3339Nano, v); perr == nil {
			b.CreatedAt = t
		}
	}
	if v, ok := res["expires_at"]; ok {
		if t, perr := time.Parse(time.RFC3339Nano, v); perr == nil {
			b.ExpiresAt = t
		}
	}
	if !b.ExpiresAt.IsZero() && r.now().After(b.ExpiresAt) {
		_ = r.Invalidate(ctx, customerID, destination)
		return nil, ErrNotFound
	}
	return b, nil
}

// Put implements Store.
func (r *Redis) Put(ctx context.Context, b Binding) error {
	if b.ExpiresAt.IsZero() {
		b.ExpiresAt = r.now().Add(r.ttl)
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = r.now()
	}
	k := redisKey(b.CustomerID, b.Destination)
	pipe := r.rdb.TxPipeline()
	pipe.HSet(ctx, k, map[string]any{
		"customer_id": b.CustomerID,
		"destination": b.Destination,
		"provider_id": b.ProviderID,
		"created_at":  b.CreatedAt.Format(time.RFC3339Nano),
		"expires_at":  b.ExpiresAt.Format(time.RFC3339Nano),
	})
	pipe.PExpireAt(ctx, k, b.ExpiresAt)
	_, err := pipe.Exec(ctx)
	return err
}

// Invalidate implements Store.
func (r *Redis) Invalidate(ctx context.Context, customerID, destination string) error {
	return r.rdb.Del(ctx, redisKey(customerID, destination)).Err()
}
