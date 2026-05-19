package status

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/incidents"
)

// AdminAuth wraps mutating incident routes with a shared-secret check.
//
// The token is read from $ADMIN_TOKEN at process boot. Empty token in
// the config means the routes are DISABLED — they return 503 — which
// is the safe default for any environment that forgot to set it.
//
// We compare with a constant-time-ish length-first check to avoid
// trivial timing oracles; the threat model here is "internal operator
// scripts", not "remote adversary", so we don't bother with crypto-
// grade comparison.
func AdminAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				http.Error(w, "admin routes disabled (ADMIN_TOKEN unset)", http.StatusServiceUnavailable)
				return
			}
			auth := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			supplied := auth[len(prefix):]
			if len(supplied) != len(token) || supplied != token {
				http.Error(w, "invalid admin token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CreateIncidentHandler handles POST /status/incidents.
func CreateIncidentHandler(store incidents.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in incidents.CreateIncidentInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		inc, err := store.CreateIncident(r.Context(), in)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, inc)
	}
}

// AppendIncidentUpdateHandler handles POST /status/incidents/{id}/updates.
func AppendIncidentUpdateHandler(store incidents.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid incident id", http.StatusBadRequest)
			return
		}
		var in incidents.UpdateIncidentInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		u, err := store.AppendUpdate(r.Context(), id, in)
		if err != nil {
			if errors.Is(err, incidents.ErrNotFound) {
				http.Error(w, "incident not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, u)
	}
}

// SubscribeHandler handles POST /status/subscribe. Public, lightly
// rate-limited via the simple bucket below to discourage harvesters.
//
// The handler does NOT itself send any email — it just registers the
// subscription. A separate notification worker reads the table and
// fans out. That keeps the request path independent from SMTP
// availability.
func SubscribeHandler(store incidents.Store) http.HandlerFunc {
	bucket := newRateBucket(60 /* tokens */, time.Minute /* refill window */)
	return func(w http.ResponseWriter, r *http.Request) {
		// Coarse per-IP rate-limit — public endpoint, no auth.
		if !bucket.allow(clientIP(r)) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		var in incidents.SubscribeInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		sub, err := store.UpsertSubscription(r.Context(), in)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Don't leak the verify token in the public response.
		out := *sub
		out.VerifyToken = ""
		writeJSON(w, http.StatusAccepted, out)
	}
}

// ----- tiny in-process rate limiter ------------------------------------------
//
// Not a substitute for the gateway-level limiter; just enough to make
// drive-by subscribe spam expensive without adding a Redis dep.

type rateBucket struct {
	mu       sync.Mutex
	capacity int
	window   time.Duration
	entries  map[string]*bucketEntry
}

type bucketEntry struct {
	tokens int
	reset  time.Time
}

func newRateBucket(cap int, window time.Duration) *rateBucket {
	return &rateBucket{capacity: cap, window: window, entries: map[string]*bucketEntry{}}
}

func (b *rateBucket) allow(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	e, ok := b.entries[key]
	if !ok || now.After(e.reset) {
		b.entries[key] = &bucketEntry{tokens: b.capacity - 1, reset: now.Add(b.window)}
		return true
	}
	if e.tokens <= 0 {
		return false
	}
	e.tokens--
	return true
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// First value in the list.
		if comma := strings.IndexByte(v, ','); comma >= 0 {
			return strings.TrimSpace(v[:comma])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	host := r.RemoteAddr
	if colon := strings.LastIndexByte(host, ':'); colon >= 0 {
		host = host[:colon]
	}
	return host
}
