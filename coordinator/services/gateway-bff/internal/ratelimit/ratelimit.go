// Package ratelimit provides a per-key token-bucket limiter and the
// HTTP middleware that applies it.
//
// We deliberately avoid an external Redis dependency at this layer: the
// gateway-bff is the only consumer, replicas don't need to share counters
// (60 req/s per user across N replicas is fine), and the JSON envelope
// returned on a hit is far more useful than network round-trips.
//
// Production sizing: AuthedRatePerSec=60 / AnonymousRatePerSec=10 per
// docs/TECH.md SLO. The limiter holds at most one bucket per active key
// so memory grows linearly with concurrent callers; idle buckets are
// reaped after IdleTTL.
package ratelimit

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

// bucket is a single token bucket. Tokens float so we can refill
// fractionally between requests without losing precision.
type bucket struct {
	tokens    float64
	lastTick  time.Time
}

// Limiter is the shared bucket store + refill clock. Safe for
// concurrent use.
type Limiter struct {
	ratePerSec float64
	burst      float64
	idleTTL    time.Duration
	now        func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

// New returns a fresh Limiter. ratePerSec is the steady-state budget;
// burst caps the maximum simultaneous "credit" callers accumulate while
// idle. idleTTL is how long an unused bucket lingers before reaping.
func New(ratePerSec, burst int, idleTTL time.Duration) *Limiter {
	if idleTTL <= 0 {
		idleTTL = 5 * time.Minute
	}
	return &Limiter{
		ratePerSec: float64(ratePerSec),
		burst:      float64(burst),
		idleTTL:    idleTTL,
		now:        time.Now,
		buckets:    map[string]*bucket{},
	}
}

// Allow consumes one token from the bucket keyed by `key`. Returns
// (allowed, retryAfter). retryAfter is the duration the caller should
// wait before the next attempt has a chance to succeed.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, lastTick: now}
		l.buckets[key] = b
	} else {
		elapsed := now.Sub(b.lastTick).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * l.ratePerSec
			if b.tokens > l.burst {
				b.tokens = l.burst
			}
			b.lastTick = now
		}
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	// We need (1 - tokens) more to allow the next request.
	deficit := 1 - b.tokens
	retry := time.Duration(deficit / l.ratePerSec * float64(time.Second))
	return false, retry
}

// Reap drops buckets that have been idle for longer than idleTTL. A
// long-running gateway should run this periodically to bound memory.
func (l *Limiter) Reap() {
	if l == nil {
		return
	}
	cutoff := l.now().Add(-l.idleTTL)
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if b.lastTick.Before(cutoff) {
			delete(l.buckets, k)
		}
	}
}

// Size reports the number of live buckets. Used by tests.
func (l *Limiter) Size() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// --- HTTP middleware ------------------------------------------------------

// Middleware returns an http.Handler middleware that rejects callers
// over their budget with 429. Authenticated callers consume the
// `authed` limiter keyed by user id; unauthenticated callers consume
// the `anon` limiter keyed by client IP.
func Middleware(authed, anon *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var (
				limiter *Limiter
				key     string
			)
			if c, ok := auth.FromContext(r.Context()); ok {
				limiter = authed
				key = "user:" + c.UserID().String()
			} else {
				limiter = anon
				key = "ip:" + clientIP(r)
			}
			ok, retry := limiter.Allow(key)
			if !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(retry.Round(time.Second).Seconds())+1))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":         "rate_limited",
					"message":      "too many requests",
					"retry_after_s": int(retry.Round(time.Second).Seconds()) + 1,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP picks the strongest signal we trust for the caller's source
// address. The shared chi middleware already populated `RemoteAddr`
// using X-Forwarded-For when configured upstream.
func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Real-IP"); h != "" {
		return h
	}
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		// First entry is the original client.
		for i, c := range h {
			if c == ',' {
				return h[:i]
			}
		}
		return h
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
