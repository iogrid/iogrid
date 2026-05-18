// Package ratelimit implements the per-customer + per-provider rate
// limits required by docs/LEGAL.md:
//
//   - Default per-customer: 100 RPS aggregate
//   - Premium per-customer: 1000 RPS aggregate
//   - Per-provider per high-value destination: 10 RPS
//
// The algorithm is a sliding-window-log over Redis. When no Redis
// client is configured the limiter degrades to an in-memory
// approximation; production deployments must point REDIS_URL at a
// real cluster.
package ratelimit

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Tier names a customer's rate-limit class.
type Tier string

const (
	// TierDefault is the standard 100 RPS class.
	TierDefault Tier = "default"
	// TierPremium is the KYC-verified 1000 RPS class.
	TierPremium Tier = "premium"
)

// Decision is the outcome of a rate-limit check.
type Decision struct {
	// Allowed is true iff the request fits within the window.
	Allowed bool
	// Reason is a machine slug for telemetry.
	Reason string
	// RetryAfter is non-zero when blocked, indicating when the
	// caller may retry.
	RetryAfter time.Duration
	// Limit is the active per-window cap.
	Limit int
	// Used is the count consumed in the current window.
	Used int
}

// Config controls limiter limits.
type Config struct {
	// Window is the look-back window (default 1s).
	Window time.Duration
	// DefaultCustomerRate is per-customer aggregate cap (default 100).
	DefaultCustomerRate int
	// PremiumCustomerRate is the premium-tier cap (default 1000).
	PremiumCustomerRate int
	// HighValueProviderRate is the per-provider per-destination cap
	// for high-value targets (default 10).
	HighValueProviderRate int
	// HighValueTargets is the list of destinations subject to the
	// 10 RPS-per-provider cap.
	HighValueTargets []string
}

// Limiter is the public type.
type Limiter struct {
	cfg    Config
	client *redis.Client
	hvSet  map[string]struct{}

	mu     sync.Mutex
	memlog map[string][]time.Time
}

// New constructs a Limiter. When rdb is nil the limiter operates in
// in-memory mode (fine for tests, NOT for production multi-replica).
func New(cfg Config, rdb *redis.Client) *Limiter {
	if cfg.Window <= 0 {
		cfg.Window = time.Second
	}
	if cfg.DefaultCustomerRate <= 0 {
		cfg.DefaultCustomerRate = 100
	}
	if cfg.PremiumCustomerRate <= 0 {
		cfg.PremiumCustomerRate = 1000
	}
	if cfg.HighValueProviderRate <= 0 {
		cfg.HighValueProviderRate = 10
	}
	hv := map[string]struct{}{}
	for _, t := range cfg.HighValueTargets {
		hv[strings.ToLower(t)] = struct{}{}
	}
	return &Limiter{
		cfg:    cfg,
		client: rdb,
		hvSet:  hv,
		memlog: map[string][]time.Time{},
	}
}

// IsHighValue reports whether destination is on the per-provider
// 10 RPS list. Match is by eTLD+1 suffix (e.g. "ads.linkedin.com" maps
// to "linkedin.com").
func (l *Limiter) IsHighValue(destination string) bool {
	d := strings.ToLower(strings.TrimSpace(destination))
	if d == "" {
		return false
	}
	for hv := range l.hvSet {
		if d == hv || strings.HasSuffix(d, "."+hv) {
			return true
		}
	}
	return false
}

// HighValueTargets returns the configured high-value list (for
// ListFilters mirroring).
func (l *Limiter) HighValueTargets() []string {
	out := make([]string, 0, len(l.hvSet))
	for k := range l.hvSet {
		out = append(out, k)
	}
	return out
}

// CheckCustomer consumes one slot in the per-customer window.
func (l *Limiter) CheckCustomer(ctx context.Context, customerID string, tier Tier) Decision {
	limit := l.cfg.DefaultCustomerRate
	if tier == TierPremium {
		limit = l.cfg.PremiumCustomerRate
	}
	key := "abuse:rl:customer:" + customerID
	return l.consume(ctx, key, limit, "customer_rate_limited")
}

// CheckProviderDestination consumes one slot in the per-provider
// per-destination window for high-value targets. Non-high-value
// destinations short-circuit to ALLOW.
func (l *Limiter) CheckProviderDestination(ctx context.Context, providerID, destination string) Decision {
	if !l.IsHighValue(destination) {
		return Decision{Allowed: true, Reason: "not_high_value"}
	}
	key := "abuse:rl:provider:" + providerID + ":dst:" + strings.ToLower(destination)
	return l.consume(ctx, key, l.cfg.HighValueProviderRate, "provider_destination_rate_limited")
}

// consume runs the sliding-window-log algorithm on the configured
// backend.
func (l *Limiter) consume(ctx context.Context, key string, limit int, blockReason string) Decision {
	if l.client == nil {
		return l.consumeMem(key, limit, blockReason)
	}
	return l.consumeRedis(ctx, key, limit, blockReason)
}

func (l *Limiter) consumeMem(key string, limit int, blockReason string) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.cfg.Window)
	// Drop expired entries.
	w := l.memlog[key]
	keep := w[:0]
	for _, t := range w {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= limit {
		oldest := keep[0]
		return Decision{
			Allowed:    false,
			Reason:     blockReason,
			RetryAfter: l.cfg.Window - now.Sub(oldest),
			Limit:      limit,
			Used:       len(keep),
		}
	}
	keep = append(keep, now)
	l.memlog[key] = keep
	return Decision{Allowed: true, Reason: "ok", Limit: limit, Used: len(keep)}
}

// consumeRedis implements the sliding-window-log against Redis using
// a sorted set keyed by the bucket name.
func (l *Limiter) consumeRedis(ctx context.Context, key string, limit int, blockReason string) Decision {
	now := time.Now()
	cutoff := now.Add(-l.cfg.Window)
	pipe := l.client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(cutoff.UnixNano(), 10))
	zadd := pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixNano()), Member: strconv.FormatInt(now.UnixNano(), 10)})
	zcard := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, l.cfg.Window*2)
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		// On Redis error, fall back to mem so we still throttle.
		return l.consumeMem(key, limit, blockReason)
	}
	_ = zadd.Val()
	used := int(zcard.Val())
	if used > limit {
		// Pop the entry we just added so retries don't accrue.
		_ = l.client.ZRem(ctx, key, strconv.FormatInt(now.UnixNano(), 10)).Err()
		oldest := l.client.ZRangeWithScores(ctx, key, 0, 0).Val()
		retry := l.cfg.Window
		if len(oldest) > 0 {
			retry = l.cfg.Window - time.Since(time.Unix(0, int64(oldest[0].Score)))
			if retry < 0 {
				retry = 0
			}
		}
		return Decision{
			Allowed:    false,
			Reason:     blockReason,
			RetryAfter: retry,
			Limit:      limit,
			Used:       used - 1,
		}
	}
	return Decision{Allowed: true, Reason: "ok", Limit: limit, Used: used}
}
