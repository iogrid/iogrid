// Package ratelimit implements a Redis-backed fixed-window rate limiter
// for the magic-link endpoint. We deliberately use a fixed window (not a
// sliding log) so the bucket math is one INCR + EXPIRE, which the
// Redis cluster handles at >50k ops/s even on a single shard.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter is the interface every rate-limit consumer talks to. The
// production impl uses Redis; tests substitute an in-memory variant.
type Limiter interface {
	// Allow returns (allowed, retryAfter, error). When allowed=false,
	// retryAfter holds the duration the caller should wait before
	// retrying — useful for surfacing in HTTP 429 headers.
	Allow(ctx context.Context, key string, max int, window time.Duration) (bool, time.Duration, error)
}

// ErrLimiterUnavailable wraps any underlying transport / connection error
// so callers can decide whether to fail open or closed.
var ErrLimiterUnavailable = errors.New("ratelimit: limiter unavailable")

// RedisLimiter is the production implementation. Increment + Expire is
// done atomically via a single MULTI block.
type RedisLimiter struct {
	Client *redis.Client
	// Prefix is prepended to every key so this limiter's namespace is
	// isolated from other Redis users.
	Prefix string
}

// NewRedis builds a RedisLimiter. URL is a redis:// connection string.
func NewRedis(url string, prefix string) (*RedisLimiter, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return &RedisLimiter{Client: redis.NewClient(opts), Prefix: prefix}, nil
}

// Allow implements Limiter.
func (l *RedisLimiter) Allow(ctx context.Context, key string, max int, window time.Duration) (bool, time.Duration, error) {
	if l == nil || l.Client == nil {
		// Fail open when no limiter wired (dev mode without Redis).
		return true, 0, nil
	}
	fullKey := fmt.Sprintf("%s:%s", l.Prefix, key)

	pipe := l.Client.TxPipeline()
	incr := pipe.Incr(ctx, fullKey)
	pipe.Expire(ctx, fullKey, window)
	ttl := pipe.TTL(ctx, fullKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, fmt.Errorf("%w: %v", ErrLimiterUnavailable, err)
	}
	count := incr.Val()
	if count <= int64(max) {
		return true, 0, nil
	}
	retry := ttl.Val()
	if retry <= 0 {
		retry = window
	}
	return false, retry, nil
}

// --- in-memory limiter (tests / dev) --------------------------------------

// MemoryLimiter is a process-local fallback used by tests. It is NOT safe
// across pods — production paths always use RedisLimiter.
type MemoryLimiter struct {
	mu      sync.Mutex
	buckets map[string]*memBucket
}

type memBucket struct {
	count   int
	expires time.Time
}

// NewMemory constructs an in-memory limiter.
func NewMemory() *MemoryLimiter {
	return &MemoryLimiter{buckets: make(map[string]*memBucket)}
}

// Allow implements Limiter.
func (m *MemoryLimiter) Allow(_ context.Context, key string, max int, window time.Duration) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	b, ok := m.buckets[key]
	if !ok || now.After(b.expires) {
		m.buckets[key] = &memBucket{count: 1, expires: now.Add(window)}
		return true, 0, nil
	}
	b.count++
	if b.count <= max {
		return true, 0, nil
	}
	return false, time.Until(b.expires), nil
}
