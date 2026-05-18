// Package customer is the in-memory snapshot of the consumer-VPN customer
// database the data plane reads on every handshake.
//
// Source of truth is identity-svc + billing-svc; vpn-gateway pulls
// snapshots on a schedule (or via NATS JetStream KV invalidation) and
// answers WireGuard handshakes against the local map at line rate. The
// data plane MUST NOT make a synchronous RPC per UDP packet — the entire
// hot path here is a sync.RWMutex read.
package customer

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/tier"
)

// Customer is one consumer VPN subscriber. PubKey is the WireGuard public
// key (32 raw bytes, base64 over the wire); we index on its hex form so
// the map keys are stable strings.
//
// AssignedIP is the /32 inside the VPN tunnel (10.99.0.0/16 by default —
// 65k customers fit per gateway pod; we shard horizontally beyond that).
// Country is the customer's currently-selected exit country (ISO alpha-2).
type Customer struct {
	ID         string    // identity-svc user UUID (string form)
	PubKey     [32]byte  // WireGuard public key
	AssignedIP string    // 10.99.X.Y/32 inside the tunnel
	Tier       tier.Tier // FREE / PLUS / PRO
	Country    string    // currently-selected exit country, ISO alpha-2 ("US")
	UpdatedAt  time.Time // last time identity-svc/billing-svc refreshed this row
}

// Registry holds a customer set + secondary index by pubkey. Safe for
// concurrent reads. Writes (Upsert / Remove / Snapshot) take the write
// lock; we expect writes to be measured in tens-per-second, reads in
// thousands.
type Registry struct {
	mu        sync.RWMutex
	byID      map[string]*Customer
	byPubKey  map[string]*Customer
	updatedAt time.Time
}

// New constructs an empty registry.
func New() *Registry {
	return &Registry{
		byID:     map[string]*Customer{},
		byPubKey: map[string]*Customer{},
	}
}

// Upsert inserts or updates a customer. Returns an error if AssignedIP is
// already taken by a different customer (we keep the IP assignment unique).
//
// Empty ID or zero-pubkey is rejected — callers must ensure a complete
// row from identity-svc/billing-svc before pushing into the data plane.
func (r *Registry) Upsert(c Customer) error {
	if c.ID == "" {
		return errors.New("customer: empty ID")
	}
	if c.PubKey == ([32]byte{}) {
		return errors.New("customer: empty pubkey")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := hex.EncodeToString(c.PubKey[:])
	if existing, ok := r.byPubKey[key]; ok && existing.ID != c.ID {
		return fmt.Errorf("customer: pubkey collision between %s and %s", existing.ID, c.ID)
	}
	// Snapshot copy so callers cannot mutate after handing off.
	cp := c
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = time.Now().UTC()
	}
	// If we already had this ID under a different pubkey, drop the
	// stale pubkey index.
	if prev, ok := r.byID[c.ID]; ok {
		prevKey := hex.EncodeToString(prev.PubKey[:])
		if prevKey != key {
			delete(r.byPubKey, prevKey)
		}
	}
	r.byID[c.ID] = &cp
	r.byPubKey[key] = &cp
	r.updatedAt = time.Now().UTC()
	return nil
}

// Remove drops a customer by ID. No-op if absent.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.byID[id]; ok {
		delete(r.byPubKey, hex.EncodeToString(c.PubKey[:]))
		delete(r.byID, id)
		r.updatedAt = time.Now().UTC()
	}
}

// ByPubKey looks up a customer by their WireGuard public key. Returns
// (nil, false) on miss.
func (r *Registry) ByPubKey(pk [32]byte) (*Customer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byPubKey[hex.EncodeToString(pk[:])]
	if !ok {
		return nil, false
	}
	cp := *c // defensive copy
	return &cp, true
}

// ByID looks up a customer by their identity-svc user ID.
func (r *Registry) ByID(id string) (*Customer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}

// Len reports the number of customers currently in the registry. Useful
// for /metrics exposition.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byID)
}

// LastUpdated returns the wall-clock time of the most recent write.
func (r *Registry) LastUpdated() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.updatedAt
}

// ReplaceAll atomically swaps the whole registry for a fresh snapshot.
// Used by the periodic-pull refresh path; callers pass the full customer
// list from billing-svc and we throw away the previous map.
func (r *Registry) ReplaceAll(cs []Customer) error {
	byID := make(map[string]*Customer, len(cs))
	byPubKey := make(map[string]*Customer, len(cs))
	for _, c := range cs {
		if c.ID == "" || c.PubKey == ([32]byte{}) {
			continue
		}
		cp := c
		if cp.UpdatedAt.IsZero() {
			cp.UpdatedAt = time.Now().UTC()
		}
		key := hex.EncodeToString(cp.PubKey[:])
		if _, dup := byPubKey[key]; dup {
			return fmt.Errorf("customer: duplicate pubkey in snapshot: %s", key)
		}
		byID[cp.ID] = &cp
		byPubKey[key] = &cp
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID = byID
	r.byPubKey = byPubKey
	r.updatedAt = time.Now().UTC()
	return nil
}

// DecodePubKey parses a base64-or-hex-encoded WG public key into a fixed
// 32-byte array. Accepts the standard WireGuard "wg pubkey" base64 form
// (44 chars w/ trailing =), and 64-char hex for convenience.
func DecodePubKey(s string) ([32]byte, error) {
	var out [32]byte
	s = strings.TrimSpace(s)
	if len(s) == 64 {
		b, err := hex.DecodeString(s)
		if err != nil {
			return out, fmt.Errorf("decode hex pubkey: %w", err)
		}
		copy(out[:], b)
		return out, nil
	}
	b, err := decodeBase64NoTrail(s)
	if err != nil {
		return out, fmt.Errorf("decode pubkey: %w", err)
	}
	if len(b) != 32 {
		return out, fmt.Errorf("pubkey length = %d, want 32", len(b))
	}
	copy(out[:], b)
	return out, nil
}

// decodeBase64NoTrail decodes std b64 tolerating missing padding.
func decodeBase64NoTrail(s string) ([]byte, error) {
	// pad to multiple of 4
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return stdB64.DecodeString(s)
}
