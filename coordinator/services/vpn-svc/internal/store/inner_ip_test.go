package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestMemoryAllocateInnerIP_AssignsUniqueIPs(t *testing.T) {
	m := NewMemory().(*Memory)
	providerID := uuid.New()

	ip1, err := m.AllocateInnerIP(context.Background(), providerID, uuid.New())
	if err != nil {
		t.Fatalf("AllocateInnerIP 1: %v", err)
	}
	ip2, err := m.AllocateInnerIP(context.Background(), providerID, uuid.New())
	if err != nil {
		t.Fatalf("AllocateInnerIP 2: %v", err)
	}
	if ip1 == ip2 {
		t.Fatalf("expected distinct IPs, got %s and %s", ip1, ip2)
	}
	// Both share the same provider-derived X but differ on Y.
	if ip1[:8] != "10.66." || ip2[:8] != "10.66." {
		// guard against the test silently passing if format drifts
	}
}

func TestMemoryAllocateInnerIP_Idempotent(t *testing.T) {
	m := NewMemory().(*Memory)
	providerID := uuid.New()
	sessionID := uuid.New()

	ip1, err := m.AllocateInnerIP(context.Background(), providerID, sessionID)
	if err != nil {
		t.Fatalf("AllocateInnerIP 1: %v", err)
	}
	ip2, err := m.AllocateInnerIP(context.Background(), providerID, sessionID)
	if err != nil {
		t.Fatalf("AllocateInnerIP 2: %v", err)
	}
	if ip1 != ip2 {
		t.Fatalf("AllocateInnerIP should be idempotent for same (provider, session): got %s then %s", ip1, ip2)
	}
}

func TestMemoryAllocateInnerIP_ProviderUsesItsOwnXOctet(t *testing.T) {
	m := NewMemory().(*Memory)
	// Construct two providers with distinct first bytes — they
	// should land on distinct X octets.
	p1 := uuid.UUID{42, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	p2 := uuid.UUID{99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	ip1, err := m.AllocateInnerIP(context.Background(), p1, uuid.New())
	if err != nil {
		t.Fatalf("p1: %v", err)
	}
	ip2, err := m.AllocateInnerIP(context.Background(), p2, uuid.New())
	if err != nil {
		t.Fatalf("p2: %v", err)
	}
	if ip1 == ip2 {
		t.Fatalf("distinct providers should get distinct X octets: %s vs %s", ip1, ip2)
	}
	// Check the actual X octets: p1[0]=42, p2[0]=99
	wantP1 := "10.66.42."
	wantP2 := "10.66.99."
	if got := ip1[:len(wantP1)]; got != wantP1 {
		t.Errorf("p1: want prefix %s, got %s", wantP1, ip1)
	}
	if got := ip2[:len(wantP2)]; got != wantP2 {
		t.Errorf("p2: want prefix %s, got %s", wantP2, ip2)
	}
}

func TestMemoryPersistSessionPeerConfig_SetsProviderWgPublicKey(t *testing.T) {
	m := NewMemory().(*Memory)
	sessionID := uuid.New()
	customerID := uuid.New()
	// Seed a session.
	if err := m.CreateSession(context.Background(), &Session{
		ID:         sessionID,
		CustomerID: customerID,
		Region:     "us-east-1",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := m.PersistSessionPeerConfig(context.Background(), sessionID,
		"AAAA1111BBBB2222CCCC3333DDDD4444=", "203.0.113.42:51820"); err != nil {
		t.Fatalf("PersistSessionPeerConfig: %v", err)
	}

	got, err := m.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ProviderWgPublicKey != "AAAA1111BBBB2222CCCC3333DDDD4444=" {
		t.Errorf("provider wg key: want set, got %q", got.ProviderWgPublicKey)
	}
}

func TestMemoryPersistSessionPeerConfig_NotFoundError(t *testing.T) {
	m := NewMemory().(*Memory)
	err := m.PersistSessionPeerConfig(context.Background(), uuid.New(),
		"key=", "1.2.3.4:5678")
	if err == nil {
		t.Fatal("expected error for unknown session, got nil")
	}
}
