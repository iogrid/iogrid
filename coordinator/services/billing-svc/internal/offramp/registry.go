package offramp

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry is the per-process catalogue of available off-ramp
// providers. main.go (or test code) builds one at boot, calls
// Register for each adapter the operator enabled, and hands the
// resulting Registry to the routes layer.
//
// Concurrency: Register is called only at boot; the read methods
// (GetProvider, ListAvailable) take an RLock so handlers can fan in
// without contention.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	order     []string // insertion order for ListAvailable
}

// NewRegistry returns an empty Registry. Operators register providers
// next.
func NewRegistry() *Registry {
	return &Registry{
		providers: map[string]Provider{},
		order:     []string{},
	}
}

// Register adds p to the catalogue under p.Name(). Returns an error
// if a provider with the same name was already registered (operator
// misconfiguration — fail loud at boot).
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return fmt.Errorf("offramp: cannot register nil provider")
	}
	name := strings.TrimSpace(p.Name())
	if name == "" {
		return fmt.Errorf("offramp: provider has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("offramp: provider %q already registered", name)
	}
	r.providers[name] = p
	r.order = append(r.order, name)
	return nil
}

// GetProvider returns the adapter registered under name, or
// ErrUnknownProvider if no such adapter exists.
func (r *Registry) GetProvider(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.providers[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
}

// ListAvailable returns every registered provider in the order they
// were registered. The web UI displays them in this order, so register
// the default real implementation first.
func (r *Registry) ListAvailable() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.providers[name])
	}
	return out
}

// Names returns the registered provider names. Sorted for stable test
// output (different from ListAvailable's insertion order).
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.providers))
	for name := range r.providers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ParseProvidersEnv splits an OFFRAMP_PROVIDERS=val env string into
// canonical names. Comma-separated, whitespace-tolerant, case-folded.
//
//	"moonpay, sociable-cash,Coinbase" → ["moonpay","sociable-cash","coinbase"]
//
// Returns nil for an empty string.
func ParseProvidersEnv(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
