package earnings

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

type fakePublisher struct {
	mu     sync.Mutex
	got    [][]byte
	failOn int
	calls  int
}

func (p *fakePublisher) Publish(_ string, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.failOn > 0 && p.calls == p.failOn {
		return errors.New("synthetic publish failure")
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	p.got = append(p.got, cp)
	return nil
}

func mkSession(t *testing.T, st store.Store, bytesIn, bytesOut uint64) *store.Session {
	t.Helper()
	id := uuid.New()
	custID := uuid.New()
	provID := uuid.New()
	if err := st.RegisterProvider(context.Background(), &store.ProviderInfo{
		ID: provID, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(),
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	s := &store.Session{
		ID:              id,
		CustomerID:      custID,
		Region:          "us-east-1",
		PrimaryProvider: provID,
		CurrentProvider: provID,
		BytesIn:         bytesIn,
		BytesOut:        bytesOut,
		CreatedAt:       time.Now().Add(-1 * time.Hour),
		LastActivityAt:  time.Now().Add(-1 * time.Hour),
	}
	if err := st.CreateSession(context.Background(), s); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.TerminateSession(context.Background(), id, "test"); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	got, err := st.GetSession(context.Background(), id)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	return got
}

func TestBatcher_PublishesUnbilledTerminatedSessions(t *testing.T) {
	st := store.NewMemory()
	mkSession(t, st, 5*uint64(bytesPerGiB), 1*uint64(bytesPerGiB))
	mkSession(t, st, 0, 0)

	pub := &fakePublisher{}
	b, err := New(Config{Store: st, Publisher: pub})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(pub.got) != 2 {
		t.Fatalf("expected 2 publishes, got %d", len(pub.got))
	}
	var saw5GiB bool
	for _, body := range pub.got {
		var e Event
		if err := json.Unmarshal(body, &e); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if e.WorkloadType != WorkloadType {
			t.Errorf("workload_type=%q want %q", e.WorkloadType, WorkloadType)
		}
		if e.Quantity == int64(5*bytesPerGiB) {
			saw5GiB = true
			// 5 GiB × 28 ¢/GiB = 140 ¢
			if e.CostCents != 5*providerShareCentsPerGiB {
				t.Errorf("cost_cents=%d want %d", e.CostCents, 5*providerShareCentsPerGiB)
			}
		}
	}
	if !saw5GiB {
		t.Errorf("missing 5 GiB session in published set: %v", pub.got)
	}
}

func TestBatcher_SecondTickIsNoop(t *testing.T) {
	st := store.NewMemory()
	mkSession(t, st, 1024*1024, 0)
	pub := &fakePublisher{}
	b, _ := New(Config{Store: st, Publisher: pub})
	if err := b.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.got) != 1 {
		t.Fatalf("expected 1 publish across 2 ticks (billed_at gates the second), got %d", len(pub.got))
	}
}

func TestBatcher_PublishFailureKeepsBilledAtNil(t *testing.T) {
	st := store.NewMemory()
	mkSession(t, st, 1024*1024, 0)
	pub := &fakePublisher{failOn: 1}
	b, _ := New(Config{Store: st, Publisher: pub})
	_ = b.Tick(context.Background())
	pending, err := st.ListUnbilledTerminatedSessions(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Errorf("expected the failed-publish row to remain unbilled, got %d pending", len(pending))
	}
	// Next tick succeeds.
	if err := b.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.got) != 1 {
		t.Errorf("expected the retry to publish, got %d publishes total", len(pub.got))
	}
}

func TestBatcher_SkipsActiveSessions(t *testing.T) {
	st := store.NewMemory()
	id := uuid.New()
	provID := uuid.New()
	_ = st.RegisterProvider(context.Background(), &store.ProviderInfo{
		ID: provID, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(),
	})
	_ = st.CreateSession(context.Background(), &store.Session{
		ID:              id,
		CustomerID:      uuid.New(),
		Region:          "us-east-1",
		PrimaryProvider: provID,
		CurrentProvider: provID,
		BytesIn:         9999,
		CreatedAt:       time.Now(),
		LastActivityAt:  time.Now(),
	})
	pub := &fakePublisher{}
	b, _ := New(Config{Store: st, Publisher: pub})
	if err := b.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.got) != 0 {
		t.Errorf("active sessions must not be billed, got %d publishes", len(pub.got))
	}
}

func TestNew_RejectsNilDeps(t *testing.T) {
	if _, err := New(Config{Publisher: &fakePublisher{}}); err == nil {
		t.Errorf("expected error on nil store")
	}
	if _, err := New(Config{Store: store.NewMemory()}); err == nil {
		t.Errorf("expected error on nil publisher")
	}
}
