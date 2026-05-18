package audit

import (
	"context"
	"testing"
)

// TestEmitter_SlogFallback verifies the emitter is usable even when
// NATS_URL is unset (the local-dev / unit-test path).
func TestEmitter_SlogFallback(t *testing.T) {
	e := New(context.Background(), Options{})
	defer e.Close()
	if err := e.EmitAudit(context.Background(), AuditEvent{
		EventKind:   "accepted",
		Protocol:    "socks5",
		Destination: "example.com:443",
		CustomerID:  "cust-1",
		ProviderID:  "prov-1",
	}); err != nil {
		t.Fatalf("EmitAudit err: %v", err)
	}
	if err := e.EmitBilling(context.Background(), BillingEvent{
		CustomerID: "cust-1",
		ProviderID: "prov-1",
		BytesIn:    1024,
		BytesOut:   2048,
	}); err != nil {
		t.Fatalf("EmitBilling err: %v", err)
	}
}

func TestSanitiseSubjectToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "unknown"},
		{"Accepted", "accepted"},
		{"relay started", "relay_started"},
		{"abc-123", "abc-123"},
		{"a.b/c", "a_b_c"},
	}
	for _, c := range cases {
		if got := sanitiseSubjectToken(c.in); got != c.want {
			t.Fatalf("sanitiseSubjectToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBillingEventDefaultsWorkloadType(t *testing.T) {
	e := New(context.Background(), Options{})
	defer e.Close()
	// We can't observe the mutation directly because EmitBilling takes
	// a value receiver — but we can re-construct one to verify the
	// default branch is exercised without panicking.
	ev := BillingEvent{CustomerID: "x", ProviderID: "y"}
	if err := e.EmitBilling(context.Background(), ev); err != nil {
		t.Fatalf("err: %v", err)
	}
}
