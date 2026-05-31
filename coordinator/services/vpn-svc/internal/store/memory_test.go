package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
)

func TestMemoryStore_SessionLifecycle(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()

	sessionID := uuid.New()
	customerID := uuid.New()
	primaryProvider := uuid.New()

	session := &Session{
		ID:              sessionID,
		CustomerID:      customerID,
		Region:          "us-east-1",
		PrimaryProvider: primaryProvider,
		CurrentProvider: primaryProvider,
		State:           pb.VpnSessionState_CREATING,
		CreatedAt:       time.Now(),
		LastActivityAt:  time.Now(),
	}

	// Create
	if err := st.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Get
	got, err := st.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomerID != customerID {
		t.Errorf("expected customer %s, got %s", customerID, got.CustomerID)
	}

	// Update state
	if err := st.UpdateSessionState(ctx, sessionID, pb.VpnSessionState_ACTIVE); err != nil {
		t.Fatalf("UpdateSessionState failed: %v", err)
	}

	got, _ = st.GetSession(ctx, sessionID)
	if got.State != pb.VpnSessionState_ACTIVE {
		t.Errorf("expected state ACTIVE, got %v", got.State)
	}

	// Terminate
	if err := st.TerminateSession(ctx, sessionID, "user_initiated"); err != nil {
		t.Fatalf("TerminateSession failed: %v", err)
	}

	got, _ = st.GetSession(ctx, sessionID)
	if got.TerminatedAt == nil {
		t.Error("expected TerminatedAt to be set")
	}
	if got.ExitReason != "user_initiated" {
		t.Errorf("expected exit reason 'user_initiated', got %q", got.ExitReason)
	}
}

func TestMemoryStore_SelectProvider_NoneAvailable(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()

	_, err := st.SelectProviderForSession(ctx, "us-east-1")
	if err == nil {
		t.Error("expected error when no providers available, got nil")
	}
}

func TestMemoryStore_SelectProvider_RoundRobin(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	// Register 3 healthy providers in us-east-1
	providerA := uuid.New()
	providerB := uuid.New()
	providerC := uuid.New()
	st.providers[providerA] = &ProviderInfo{ID: providerA, Region: "us-east-1", Status: "healthy", SessionCount: 0}
	st.providers[providerB] = &ProviderInfo{ID: providerB, Region: "us-east-1", Status: "healthy", SessionCount: 0}
	st.providers[providerC] = &ProviderInfo{ID: providerC, Region: "us-east-1", Status: "healthy", SessionCount: 0}

	// One in different region — should NOT be selected
	providerD := uuid.New()
	st.providers[providerD] = &ProviderInfo{ID: providerD, Region: "us-west-2", Status: "healthy", SessionCount: 0}

	// First 3 selections should hit each provider once (round-robin by session_count)
	selections := make(map[uuid.UUID]int)
	for i := 0; i < 6; i++ {
		picked, err := st.SelectProviderForSession(ctx, "us-east-1")
		if err != nil {
			t.Fatalf("SelectProviderForSession failed at iteration %d: %v", i, err)
		}
		if picked == providerD {
			t.Errorf("picked us-west-2 provider for us-east-1 request")
		}
		selections[picked]++
	}

	// Each of A, B, C should have been picked 2 times (6 / 3)
	for _, p := range []uuid.UUID{providerA, providerB, providerC} {
		if selections[p] != 2 {
			t.Errorf("expected provider %s picked 2 times, got %d", p, selections[p])
		}
	}
}

func TestMemoryStore_SelectProvider_SkipsOffline(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	offlineProv := uuid.New()
	healthyProv := uuid.New()
	st.providers[offlineProv] = &ProviderInfo{ID: offlineProv, Region: "us-east-1", Status: "offline"}
	st.providers[healthyProv] = &ProviderInfo{ID: healthyProv, Region: "us-east-1", Status: "healthy"}

	picked, err := st.SelectProviderForSession(ctx, "us-east-1")
	if err != nil {
		t.Fatalf("SelectProviderForSession failed: %v", err)
	}
	if picked != healthyProv {
		t.Errorf("expected healthy provider, got offline provider")
	}
}

func TestMemoryStore_TriggerFailover(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()

	sessionID := uuid.New()
	primaryProv := uuid.New()
	altProv := uuid.New()

	session := &Session{
		ID:              sessionID,
		CustomerID:      uuid.New(),
		Region:          "us-east-1",
		PrimaryProvider: primaryProv,
		CurrentProvider: primaryProv,
		State:           pb.VpnSessionState_ACTIVE,
		CreatedAt:       time.Now(),
		LastActivityAt:  time.Now(),
	}
	_ = st.CreateSession(ctx, session)

	// Trigger failover
	if err := st.TriggerFailover(ctx, sessionID, primaryProv, altProv); err != nil {
		t.Fatalf("TriggerFailover failed: %v", err)
	}

	got, _ := st.GetSession(ctx, sessionID)
	if got.CurrentProvider != altProv {
		t.Errorf("expected current provider %s, got %s", altProv, got.CurrentProvider)
	}
	if got.FailoverCount != 1 {
		t.Errorf("expected FailoverCount=1, got %d", got.FailoverCount)
	}
	if got.State != pb.VpnSessionState_FAILING_OVER {
		t.Errorf("expected state FAILING_OVER, got %v", got.State)
	}
}

func TestMemoryStore_UpdateProviderHealth(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	providerID := uuid.New()
	st.providers[providerID] = &ProviderInfo{ID: providerID, Region: "us-east-1", Status: "healthy"}

	now := time.Now()
	if err := st.UpdateProviderHealth(ctx, providerID, "degraded", now); err != nil {
		t.Fatalf("UpdateProviderHealth failed: %v", err)
	}

	if st.providers[providerID].Status != "degraded" {
		t.Errorf("expected status 'degraded', got %q", st.providers[providerID].Status)
	}
	if !st.providers[providerID].LastSeenAt.Equal(now) {
		t.Errorf("expected LastSeenAt updated")
	}
}

func TestMemoryStore_GetProvidersInRegion_SkipsOffline(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	healthyA := uuid.New()
	degradedB := uuid.New()
	offlineC := uuid.New()
	wrongRegion := uuid.New()
	st.providers[healthyA] = &ProviderInfo{ID: healthyA, Region: "eu-west-1", Status: "healthy"}
	st.providers[degradedB] = &ProviderInfo{ID: degradedB, Region: "eu-west-1", Status: "degraded"}
	st.providers[offlineC] = &ProviderInfo{ID: offlineC, Region: "eu-west-1", Status: "offline"}
	st.providers[wrongRegion] = &ProviderInfo{ID: wrongRegion, Region: "us-east-1", Status: "healthy"}

	providers, err := st.GetProvidersInRegion(ctx, "eu-west-1")
	if err != nil {
		t.Fatalf("GetProvidersInRegion failed: %v", err)
	}

	if len(providers) != 2 {
		t.Errorf("expected 2 providers (healthy + degraded), got %d", len(providers))
	}

	// Make sure offline + wrong-region are not in the result
	for _, p := range providers {
		if p.Status == "offline" {
			t.Error("offline provider should not be returned")
		}
		if p.Region != "eu-west-1" {
			t.Errorf("wrong-region provider returned: %v", p.Region)
		}
	}
}
