package alerter

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

// fakeStore is a tiny test-local Store implementation that only fills in
// the ListProviders surface the alerter uses. Every other method panics
// so we'd notice immediately if the alerter started leaning on more API.
type fakeStore struct {
	mu        sync.Mutex
	providers []*store.Provider
}

func (f *fakeStore) ListProviders(_ context.Context, _ store.ListOptions) ([]*store.Provider, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*store.Provider, 0, len(f.providers))
	for _, p := range f.providers {
		// Return clones so the alerter's read can't mutate the test
		// fixture by accident.
		cp := *p
		out = append(out, &cp)
	}
	return out, "", nil
}

// upsert replaces the provider with matching id (or appends if new) +
// keeps a stable order. Tests use this both for initial seed + to
// simulate a fresh heartbeat by bumping LastSeenAt.
func (f *fakeStore) upsert(p *store.Provider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, existing := range f.providers {
		if existing.ID == p.ID {
			cp := *p
			f.providers[i] = &cp
			return
		}
	}
	cp := *p
	f.providers = append(f.providers, &cp)
}

// Every other Store method panics — the alerter doesn't use them.
func (f *fakeStore) CreateProvider(context.Context, *store.Provider) error { panic("unused") }
func (f *fakeStore) UpdateProvider(context.Context, *store.Provider) error { panic("unused") }
func (f *fakeStore) GetProvider(context.Context, string) (*store.Provider, error) {
	panic("unused")
}
func (f *fakeStore) DeactivateProvider(context.Context, string, string) error { panic("unused") }
func (f *fakeStore) UpdateLastSeen(context.Context, string, time.Time) error  { panic("unused") }
func (f *fakeStore) SetPrimaryProvider(context.Context, string, string) (*store.Provider, error) {
	panic("unused")
}
func (f *fakeStore) GetProviderByOwnerAndDisplayName(context.Context, string, string) (*store.Provider, error) {
	panic("unused")
}
func (f *fakeStore) GetProviderByOwnerAndPublicKey(context.Context, string, []byte) (*store.Provider, error) {
	panic("unused")
}
func (f *fakeStore) SelectProviderForOwner(context.Context, string) (*store.Provider, error) {
	panic("unused")
}
func (f *fakeStore) IssuePairingToken(context.Context, string, time.Duration) (string, error) {
	panic("unused")
}
func (f *fakeStore) ConsumePairingToken(context.Context, string) (store.PairingToken, error) {
	panic("unused")
}
func (f *fakeStore) GetSchedulingConfig(context.Context, string) (*store.SchedulingConfig, error) {
	panic("unused")
}
func (f *fakeStore) UpdateSchedulingConfig(context.Context, *store.SchedulingConfig) (*store.SchedulingConfig, error) {
	panic("unused")
}
func (f *fakeStore) AppendAuditEvent(context.Context, store.AuditEvent) error { panic("unused") }
func (f *fakeStore) ListAuditEvents(context.Context, string, store.AuditQuery) ([]store.AuditEvent, string, error) {
	panic("unused")
}
func (f *fakeStore) SubscribeAuditEvents(string) (<-chan store.AuditEvent, func()) { panic("unused") }
func (f *fakeStore) CreditEarnings(context.Context, store.EarningsEntry) error    { panic("unused") }
func (f *fakeStore) SumEarnings(context.Context, string, time.Time, time.Time) (int64, map[string]int64, string, error) {
	panic("unused")
}

// newTestAlerter wires an alerter + fakeStore + buffer-backed slog
// handler with the alerter clock injected to a fixed instant the test
// controls.
func newTestAlerter(t *testing.T, now time.Time) (*Alerter, *fakeStore, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	lg := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st := &fakeStore{}
	a := New(st, lg, Config{
		ScanInterval:       time.Hour, // doesn't matter — tests call scanOnce directly
		StalenessThreshold: 5 * time.Minute,
		PageSize:           50,
	})
	a.now = func() time.Time { return now }
	return a, st, buf
}

// containsEvent reports whether the slog JSON buffer carries an entry
// whose msg field equals want.
func containsEvent(buf *bytes.Buffer, want string) bool {
	for _, line := range strings.Split(buf.String(), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m["msg"] == want {
			return true
		}
	}
	return false
}

func TestAlerter_FreshProviderEmitsNothing(t *testing.T) {
	now := time.Date(2026, 5, 24, 19, 0, 0, 0, time.UTC)
	a, st, buf := newTestAlerter(t, now)
	st.upsert(&store.Provider{
		ID:          "p1",
		OwnerUserID: "u1",
		DisplayName: "fresh",
		Status:      store.StatusActive,
		LastSeenAt:  now.Add(-30 * time.Second), // well inside 5min threshold
	})
	a.scanOnce(context.Background())
	if containsEvent(buf, "provider.heartbeat_loss") {
		t.Fatalf("fresh provider should not fire heartbeat_loss; got logs:\n%s", buf.String())
	}
}

func TestAlerter_StaleProviderFiresOnceThenDeduplicates(t *testing.T) {
	now := time.Date(2026, 5, 24, 19, 0, 0, 0, time.UTC)
	a, st, buf := newTestAlerter(t, now)
	st.upsert(&store.Provider{
		ID:          "p1",
		OwnerUserID: "u1",
		DisplayName: "hatice-mac-style",
		Status:      store.StatusActive,
		LastSeenAt:  now.Add(-10 * time.Minute), // beyond 5min threshold
	})
	a.scanOnce(context.Background())
	if !containsEvent(buf, "provider.heartbeat_loss") {
		t.Fatalf("stale provider should fire heartbeat_loss; got logs:\n%s", buf.String())
	}
	buf.Reset()
	a.scanOnce(context.Background())
	if containsEvent(buf, "provider.heartbeat_loss") {
		t.Fatalf("duplicate alert fired on second scan; got logs:\n%s", buf.String())
	}
}

func TestAlerter_RecoveryEmitsOnHeartbeatForward(t *testing.T) {
	now := time.Date(2026, 5, 24, 19, 0, 0, 0, time.UTC)
	a, st, buf := newTestAlerter(t, now)
	st.upsert(&store.Provider{
		ID:          "p1",
		OwnerUserID: "u1",
		DisplayName: "recovering",
		Status:      store.StatusActive,
		LastSeenAt:  now.Add(-10 * time.Minute),
	})
	a.scanOnce(context.Background())
	if !containsEvent(buf, "provider.heartbeat_loss") {
		t.Fatalf("setup: expected initial heartbeat_loss; got:\n%s", buf.String())
	}
	buf.Reset()
	// Daemon comes back: last_seen_at moves forward + inside threshold.
	st.upsert(&store.Provider{
		ID:          "p1",
		OwnerUserID: "u1",
		DisplayName: "recovering",
		Status:      store.StatusActive,
		LastSeenAt:  now.Add(-30 * time.Second),
	})
	a.scanOnce(context.Background())
	if !containsEvent(buf, "provider.heartbeat_recovered") {
		t.Fatalf("expected heartbeat_recovered after fresh heartbeat; got:\n%s", buf.String())
	}
	buf.Reset()
	a.scanOnce(context.Background())
	if containsEvent(buf, "provider.heartbeat_recovered") {
		t.Fatalf("recovery should not re-fire on subsequent scans; got:\n%s", buf.String())
	}
	if containsEvent(buf, "provider.heartbeat_loss") {
		t.Fatalf("loss should not fire after recovery; got:\n%s", buf.String())
	}
}

func TestAlerter_ZeroLastSeenIsSkipped(t *testing.T) {
	now := time.Date(2026, 5, 24, 19, 0, 0, 0, time.UTC)
	a, st, buf := newTestAlerter(t, now)
	st.upsert(&store.Provider{
		ID:          "p1",
		OwnerUserID: "u1",
		DisplayName: "just-paired",
		Status:      store.StatusActive,
		// LastSeenAt left zero — fresh pair, no heartbeat yet.
	})
	a.scanOnce(context.Background())
	if containsEvent(buf, "provider.heartbeat_loss") {
		t.Fatalf("provider with zero LastSeenAt should be skipped; got:\n%s", buf.String())
	}
}
