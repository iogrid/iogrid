package sessions

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemory_PutGet(t *testing.T) {
	m := NewMemory(time.Minute)
	if err := m.Put(context.Background(), Binding{
		CustomerID:  "cust-1",
		Destination: "example.com:443",
		ProviderID:  "prov-1",
	}); err != nil {
		t.Fatalf("Put err: %v", err)
	}
	b, err := m.Get(context.Background(), "cust-1", "example.com:443")
	if err != nil {
		t.Fatalf("Get err: %v", err)
	}
	if b.ProviderID != "prov-1" {
		t.Fatalf("provider = %q", b.ProviderID)
	}
	if b.ExpiresAt.IsZero() {
		t.Fatalf("ExpiresAt not set")
	}
}

func TestMemory_GetNotFound(t *testing.T) {
	m := NewMemory(time.Minute)
	if _, err := m.Get(context.Background(), "x", "y"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMemory_Expiry(t *testing.T) {
	m := NewMemory(time.Millisecond)
	fakeNow := time.Now()
	m.now = func() time.Time { return fakeNow }
	_ = m.Put(context.Background(), Binding{CustomerID: "c", Destination: "d", ProviderID: "p"})
	// Advance fake clock past TTL.
	fakeNow = fakeNow.Add(time.Hour)
	if _, err := m.Get(context.Background(), "c", "d"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected expiry to evict; got %v", err)
	}
}

func TestMemory_Invalidate(t *testing.T) {
	m := NewMemory(time.Minute)
	_ = m.Put(context.Background(), Binding{CustomerID: "c", Destination: "d", ProviderID: "p"})
	if err := m.Invalidate(context.Background(), "c", "d"); err != nil {
		t.Fatalf("invalidate err: %v", err)
	}
	if _, err := m.Get(context.Background(), "c", "d"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected NotFound after invalidate; got %v", err)
	}
}

func TestMemory_CaseInsensitive(t *testing.T) {
	m := NewMemory(time.Minute)
	_ = m.Put(context.Background(), Binding{CustomerID: "Cust-A", Destination: "Example.COM:443", ProviderID: "p"})
	if _, err := m.Get(context.Background(), "cust-a", "example.com:443"); err != nil {
		t.Fatalf("case-insensitive Get failed: %v", err)
	}
}
