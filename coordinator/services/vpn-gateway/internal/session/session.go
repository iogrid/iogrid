// Package session tracks the per-customer sticky exit-provider binding.
//
// When a customer's first outbound packet arrives, we pick a provider
// matching their selected country and pin that customer→provider mapping
// for 15 minutes (default). Subsequent packets in the same window flow
// through the same provider. The window is rolling — every packet
// extends the binding's deadline.
//
// Stickiness matters for:
//   - HTTP session continuity (sites that bind to client IP)
//   - Anti-bot evasion failure (rotating IPs trip bot detectors)
//   - Latency stability (one provider's geo-path is consistent)
//
// 15 minutes is the same TTL the customer-facing proxy uses.
package session

import (
	"sync"
	"time"
)

// Binding is one customer's pinned exit choice.
type Binding struct {
	CustomerID string
	ProviderID string
	Country    string
	ExpiresAt  time.Time
}

// Store is the in-memory binding table. Backed by Redis in production
// for cross-pod stickiness; this in-memory implementation is the canonical
// reference + the fallback when Redis is unreachable.
type Store struct {
	mu    sync.Mutex
	byID  map[string]*Binding
	ttl   time.Duration
	clock func() time.Time
}

// New returns a Store with the supplied stickiness window. Zero TTL
// uses the production default of 15 minutes.
func New(ttl time.Duration) *Store {
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	return &Store{
		byID:  map[string]*Binding{},
		ttl:   ttl,
		clock: time.Now,
	}
}

// WithClock replaces the clock — for deterministic tests only.
func (s *Store) WithClock(c func() time.Time) *Store {
	s.clock = c
	return s
}

// Get returns the current binding for customerID, if any, after
// applying expiry. Returns (nil, false) on miss-or-expired.
func (s *Store) Get(customerID string) (*Binding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.byID[customerID]
	if !ok {
		return nil, false
	}
	if !s.clock().Before(b.ExpiresAt) {
		// expired — drop and miss
		delete(s.byID, customerID)
		return nil, false
	}
	cp := *b
	return &cp, true
}

// Bind installs (or extends) a sticky binding. Provider switching is
// allowed: if the country changes, we overwrite the binding outright;
// otherwise we extend the existing deadline. Returns the post-update
// binding (always non-nil).
func (s *Store) Bind(customerID, providerID, country string) *Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	exp := now.Add(s.ttl)
	if b, ok := s.byID[customerID]; ok && now.Before(b.ExpiresAt) && b.Country == country {
		// Same country, in-window — extend.
		b.ProviderID = providerID
		b.ExpiresAt = exp
		cp := *b
		return &cp
	}
	b := &Binding{
		CustomerID: customerID,
		ProviderID: providerID,
		Country:    country,
		ExpiresAt:  exp,
	}
	s.byID[customerID] = b
	cp := *b
	return &cp
}

// Drop removes a customer's binding (used on logout / kill switch /
// admin force-rebalance).
func (s *Store) Drop(customerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, customerID)
}

// Len returns the count of live bindings. Includes expired-but-not-yet-
// reaped entries; pair with Sweep for a precise number.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byID)
}

// Sweep evicts expired bindings. Cheap O(N); the caller is expected
// to invoke it on a ~minute cadence.
func (s *Store) Sweep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	dropped := 0
	for id, b := range s.byID {
		if !now.Before(b.ExpiresAt) {
			delete(s.byID, id)
			dropped++
		}
	}
	return dropped
}
