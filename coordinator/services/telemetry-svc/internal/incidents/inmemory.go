package incidents

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InMemory is the in-process Store implementation. Goroutine-safe via
// a single mutex; the data volume on the status page (a few dozen
// incidents per quarter, ~10 services x 90 days of samples) is so
// modest that we do not bother sharding.
//
// State is lost on process restart. Production binaries use [Postgres];
// [InMemory] is for unit tests and the local-dev binary (no
// DATABASE_URL).
type InMemory struct {
	mu            sync.Mutex
	incidents     map[uuid.UUID]*Incident
	updates       map[uuid.UUID][]Update // keyed by incident ID
	subscriptions map[string]*Subscription
	samples       map[string]UptimeSample // key = "service|day"
	// now is the clock source — overridable in tests.
	now func() time.Time
}

// NewInMemory returns an empty in-memory store.
func NewInMemory() *InMemory {
	return &InMemory{
		incidents:     map[uuid.UUID]*Incident{},
		updates:       map[uuid.UUID][]Update{},
		subscriptions: map[string]*Subscription{},
		samples:       map[string]UptimeSample{},
		now:           func() time.Time { return time.Now().UTC() },
	}
}

// SetClock replaces the time source. Tests use this to drive
// deterministic timestamps without sleeping.
func (m *InMemory) SetClock(now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}

// CreateIncident inserts a new incident + a seed update echoing the
// initial status.
func (m *InMemory) CreateIncident(_ context.Context, in CreateIncidentInput) (*Incident, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("incidents: title required")
	}
	if in.Status == "" {
		in.Status = StatusInvestigating
	}
	if !in.Status.Valid() {
		return nil, fmt.Errorf("incidents: invalid status %q", in.Status)
	}
	if in.Impact == "" {
		in.Impact = ImpactMinor
	}
	if !in.Impact.Valid() {
		return nil, fmt.Errorf("incidents: invalid impact %q", in.Impact)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	id := uuid.New()
	inc := &Incident{
		ID:               id,
		Title:            strings.TrimSpace(in.Title),
		Body:             in.Body,
		Status:           in.Status,
		Impact:           in.Impact,
		AffectedServices: append([]string(nil), in.AffectedServices...),
		StartedAt:        now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if inc.Status == StatusResolved {
		t := now
		inc.ResolvedAt = &t
	}
	m.incidents[id] = inc
	seed := Update{
		ID:         uuid.New(),
		IncidentID: id,
		Status:     in.Status,
		Body:       firstNonEmpty(in.Body, fmt.Sprintf("Incident opened (status=%s, impact=%s)", in.Status, in.Impact)),
		CreatedAt:  now,
	}
	m.updates[id] = []Update{seed}
	out := *inc
	out.Updates = []Update{seed}
	return &out, nil
}

// GetIncident returns the incident by ID with Updates hydrated.
func (m *InMemory) GetIncident(_ context.Context, id uuid.UUID) (*Incident, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getLocked(id)
}

func (m *InMemory) getLocked(id uuid.UUID) (*Incident, error) {
	inc, ok := m.incidents[id]
	if !ok {
		return nil, ErrNotFound
	}
	out := *inc
	upds := append([]Update(nil), m.updates[id]...)
	sort.Slice(upds, func(i, j int) bool { return upds[i].CreatedAt.After(upds[j].CreatedAt) })
	out.Updates = upds
	return &out, nil
}

// AppendUpdate adds an Update and advances the incident status.
func (m *InMemory) AppendUpdate(_ context.Context, id uuid.UUID, in UpdateIncidentInput) (*Update, error) {
	if !in.Status.Valid() {
		return nil, fmt.Errorf("incidents: invalid status %q", in.Status)
	}
	if strings.TrimSpace(in.Body) == "" {
		return nil, fmt.Errorf("incidents: update body required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	inc, ok := m.incidents[id]
	if !ok {
		return nil, ErrNotFound
	}
	now := m.now()
	u := Update{
		ID:         uuid.New(),
		IncidentID: id,
		Status:     in.Status,
		Body:       in.Body,
		CreatedAt:  now,
	}
	m.updates[id] = append(m.updates[id], u)
	inc.Status = in.Status
	inc.UpdatedAt = now
	if in.Status == StatusResolved && inc.ResolvedAt == nil {
		t := now
		inc.ResolvedAt = &t
	}
	return &u, nil
}

// ListActive returns unresolved incidents, newest first.
func (m *InMemory) ListActive(_ context.Context) ([]Incident, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Incident, 0)
	for id, inc := range m.incidents {
		if inc.ResolvedAt != nil {
			continue
		}
		hydrated, _ := m.getLocked(id)
		out = append(out, *hydrated)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

// ListRecent returns all incidents where started_at >= now-since.
func (m *InMemory) ListRecent(_ context.Context, since time.Duration) ([]Incident, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := m.now().Add(-since)
	out := make([]Incident, 0)
	for id, inc := range m.incidents {
		if inc.StartedAt.Before(cutoff) {
			continue
		}
		hydrated, _ := m.getLocked(id)
		out = append(out, *hydrated)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

// UpsertSubscription is idempotent on (lower(email)).
func (m *InMemory) UpsertSubscription(_ context.Context, in SubscribeInput) (*Subscription, error) {
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if !looksLikeEmail(email) {
		return nil, fmt.Errorf("incidents: invalid email %q", in.Email)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.subscriptions[email]; ok && existing.UnsubscribedAt == nil {
		out := *existing
		return &out, nil
	}
	now := m.now()
	s := &Subscription{
		ID:             uuid.New(),
		Email:          email,
		Verified:       false,
		VerifyToken:    uuid.NewString(),
		ServicesFilter: append([]string(nil), in.ServicesFilter...),
		CreatedAt:      now,
	}
	m.subscriptions[email] = s
	out := *s
	return &out, nil
}

// RecordSample upserts the (service, day) sample.
func (m *InMemory) RecordSample(_ context.Context, s UptimeSample) error {
	if s.Service == "" || s.Day == "" {
		return fmt.Errorf("incidents: service + day required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples[s.Service+"|"+s.Day] = s
	return nil
}

// UptimeForService returns the past `days` days for a service, oldest
// first, with missing days filled in with State="" so the frontend
// sees a fixed-length array.
func (m *InMemory) UptimeForService(_ context.Context, service string, days int) ([]UptimeSample, error) {
	if days <= 0 {
		days = 90
	}
	if days > 365 {
		days = 365
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]UptimeSample, 0, days)
	end := m.now().UTC().Truncate(24 * time.Hour)
	for i := days - 1; i >= 0; i-- {
		day := end.Add(-time.Duration(i) * 24 * time.Hour).Format("2006-01-02")
		if s, ok := m.samples[service+"|"+day]; ok {
			out = append(out, s)
			continue
		}
		out = append(out, UptimeSample{Service: service, Day: day, State: "", SLIPct: 0})
	}
	return out, nil
}

func looksLikeEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	dot := strings.LastIndexByte(s, '.')
	return at > 0 && dot > at+1 && dot < len(s)-1
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
