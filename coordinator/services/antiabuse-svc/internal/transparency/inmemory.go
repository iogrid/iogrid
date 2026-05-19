package transparency

import (
	"context"
	"sync"
	"time"
)

// InMemory is a deterministic MetricsSource used by unit tests and as
// a fallback when no real telemetry adapter is wired. The CronJob in
// production wires the NATS-JetStream backed source; this exists so
// the generator can be exercised end-to-end without that pipeline.
type InMemory struct {
	mu sync.Mutex

	// Counts buckets events by (start..end) → category/backend counters.
	// Tests populate directly; production wraps with an event consumer.
	checks            int64
	blocksByCategory  map[string]int64
	blocksByBackend   map[string]int64
	checksByBackend   map[string]int64
	le                LawEnforcementBlock
	ar                AuditRetentionBlock
}

// NewInMemory returns an empty InMemory source.
func NewInMemory() *InMemory {
	return &InMemory{
		blocksByCategory: map[string]int64{},
		blocksByBackend:  map[string]int64{},
		checksByBackend:  map[string]int64{},
	}
}

// SetChecks fixes the total-checks figure.
func (m *InMemory) SetChecks(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checks = n
}

// AddCategory increments the per-category block counter.
func (m *InMemory) AddCategory(cat string, n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocksByCategory[cat] += n
}

// AddBackendBlocks bumps the per-backend block counter.
func (m *InMemory) AddBackendBlocks(backend string, n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocksByBackend[backend] += n
}

// SetBackendChecks sets the per-backend total-check denominator.
func (m *InMemory) SetBackendChecks(backend string, n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checksByBackend[backend] = n
}

// SetLawEnforcement replaces the LE block.
func (m *InMemory) SetLawEnforcement(le LawEnforcementBlock) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.le = le
}

// SetAuditRetention replaces the retention block.
func (m *InMemory) SetAuditRetention(ar AuditRetentionBlock) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ar = ar
}

// TotalChecks implements MetricsSource.
func (m *InMemory) TotalChecks(_ context.Context, _, _ time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checks, nil
}

// BlocksByCategory implements MetricsSource.
func (m *InMemory) BlocksByCategory(_ context.Context, _, _ time.Time) (map[string]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int64, len(m.blocksByCategory))
	for k, v := range m.blocksByCategory {
		out[k] = v
	}
	return out, nil
}

// BlocksByBackend implements MetricsSource.
func (m *InMemory) BlocksByBackend(_ context.Context, _, _ time.Time) (map[string]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int64, len(m.blocksByBackend))
	for k, v := range m.blocksByBackend {
		out[k] = v
	}
	return out, nil
}

// ChecksByBackend implements MetricsSource.
func (m *InMemory) ChecksByBackend(_ context.Context, _, _ time.Time) (map[string]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int64, len(m.checksByBackend))
	for k, v := range m.checksByBackend {
		out[k] = v
	}
	return out, nil
}

// LawEnforcement implements MetricsSource.
func (m *InMemory) LawEnforcement(_ context.Context, _, _ time.Time) (LawEnforcementBlock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.le, nil
}

// AuditRetention implements MetricsSource.
func (m *InMemory) AuditRetention(_ context.Context) (AuditRetentionBlock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ar, nil
}
