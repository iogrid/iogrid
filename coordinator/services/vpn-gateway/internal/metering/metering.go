// Package metering accumulates per-customer byte counters and emits
// BILLING events to NATS JetStream.
//
// The data plane calls AddBytes after every WireGuard packet (or, in
// practice, every N packets — the WG frontend batches the count to
// avoid contending on the meter lock per packet). A flusher goroutine
// drains the meter on a fixed cadence and ships rollups to JetStream.
//
// Free-tier enforcement reads MonthToDate(customer) and rejects new
// packets when > tier cap; the WG layer drops the packet and pushes an
// upgrade-prompt message back to the client.
package metering

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Counter is one customer's running tally.
type Counter struct {
	BytesIn  uint64
	BytesOut uint64
	// FlushedAt is the wall time the last roll-up was emitted to NATS.
	FlushedAt time.Time
}

// Total returns in + out bytes.
func (c Counter) Total() uint64 { return c.BytesIn + c.BytesOut }

// Meter is the in-memory accumulator. The bytes counters use atomic add
// so the WG frontend can increment without taking the map lock; the
// lock is needed only when (re-)constructing a Counter entry.
type Meter struct {
	mu       sync.RWMutex
	counters map[string]*atomicPair
	// monthStart is the rollover boundary. We zero counters at the
	// start of each calendar month (UTC).
	monthStart time.Time
	// emitter is the function called per rollup; in production this is
	// the JetStream publisher.
	emitter Emitter
	clock   func() time.Time
}

type atomicPair struct {
	in        atomic.Uint64
	out       atomic.Uint64
	flushedAt atomic.Int64 // unix nano
}

// Emitter is the side-effect signature the meter calls on each flush.
// Errors are best-effort; the meter does not block on emission and does
// not retry — the receiver (billing-svc) is responsible for late-arrival
// reconciliation.
type Emitter func(ctx context.Context, ev Event) error

// Event is the JetStream BILLING payload for one (customer, period)
// roll-up. Subject in production: BILLING.vpn.usage.<tier>.<customer_id>
type Event struct {
	CustomerID string    `json:"customer_id"`
	Tier       string    `json:"tier"`
	BytesIn    uint64    `json:"bytes_in"`
	BytesOut   uint64    `json:"bytes_out"`
	WindowFrom time.Time `json:"window_from"`
	WindowTo   time.Time `json:"window_to"`
}

// New returns a Meter with the supplied emitter and a real clock.
// Pass nil emitter for unit tests.
func New(emitter Emitter) *Meter {
	now := time.Now().UTC()
	return &Meter{
		counters:   map[string]*atomicPair{},
		monthStart: monthStartOf(now),
		emitter:    emitter,
		clock:      func() time.Time { return time.Now().UTC() },
	}
}

// WithClock plugs a deterministic clock for tests.
func (m *Meter) WithClock(c func() time.Time) *Meter {
	m.clock = c
	m.monthStart = monthStartOf(c())
	return m
}

// AddBytes increments the customer's in/out counters atomically.
//
// This is the hot path — called once per WG packet (or per N-packet
// batch). The map lookup is under an RLock; only a fresh customer takes
// the WLock to allocate a counter.
func (m *Meter) AddBytes(customerID string, in, out uint64) {
	m.mu.RLock()
	p, ok := m.counters[customerID]
	m.mu.RUnlock()
	if !ok {
		m.mu.Lock()
		if p, ok = m.counters[customerID]; !ok {
			p = &atomicPair{}
			m.counters[customerID] = p
		}
		m.mu.Unlock()
	}
	if in > 0 {
		p.in.Add(in)
	}
	if out > 0 {
		p.out.Add(out)
	}
}

// MonthToDate returns the running counter for the customer. Returns
// zero-valued Counter on miss.
func (m *Meter) MonthToDate(customerID string) Counter {
	m.maybeRollover()
	m.mu.RLock()
	p, ok := m.counters[customerID]
	m.mu.RUnlock()
	if !ok {
		return Counter{}
	}
	return Counter{
		BytesIn:   p.in.Load(),
		BytesOut:  p.out.Load(),
		FlushedAt: time.Unix(0, p.flushedAt.Load()).UTC(),
	}
}

// FlushAll emits a rollup event for every customer with non-zero
// usage since the last flush. The supplied tierFor function resolves the
// customer's current tier (so the event tag is right).
//
// The flusher leaves the counters in place — we accumulate month-to-date.
// On month rollover, maybeRollover zeroes them.
func (m *Meter) FlushAll(ctx context.Context, tierFor func(customerID string) string) (int, error) {
	if m.emitter == nil {
		return 0, nil
	}
	m.mu.RLock()
	ids := make([]string, 0, len(m.counters))
	for id := range m.counters {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	count := 0
	now := m.clock()
	for _, id := range ids {
		m.mu.RLock()
		p := m.counters[id]
		m.mu.RUnlock()
		if p == nil {
			continue
		}
		in := p.in.Load()
		out := p.out.Load()
		if in == 0 && out == 0 {
			continue
		}
		ev := Event{
			CustomerID: id,
			Tier:       tierFor(id),
			BytesIn:    in,
			BytesOut:   out,
			WindowFrom: m.monthStart,
			WindowTo:   now,
		}
		if err := m.emitter(ctx, ev); err != nil {
			return count, err
		}
		p.flushedAt.Store(now.UnixNano())
		count++
	}
	return count, nil
}

// maybeRollover zeroes counters at month boundary (UTC). Called from
// the hot path's MonthToDate.
func (m *Meter) maybeRollover() {
	now := m.clock()
	ms := monthStartOf(now)
	if ms.After(m.monthStart) {
		m.mu.Lock()
		// Re-check after acquiring write lock.
		if ms.After(m.monthStart) {
			m.counters = map[string]*atomicPair{}
			m.monthStart = ms
		}
		m.mu.Unlock()
	}
}

// monthStartOf returns the first instant of the calendar month for t (UTC).
func monthStartOf(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
