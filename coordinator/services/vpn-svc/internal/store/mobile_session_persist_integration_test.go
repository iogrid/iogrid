//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
)

// TestPostgres_CreateSession_PersistsCustomerWgKey is the regression test
// for #701: the mobile flow (#698) sets CustomerWgPublicKey + InnerIP on the
// session AT CREATION so the daemon's binder — which polls
// /assigned-sessions — can add the device as a WG peer. The previous
// Postgres CreateSession INSERT wrote only the 8 identity/state columns and
// silently dropped customer_wg_public_key / client_public_key / inner_ip,
// so on Postgres /assigned-sessions returned an EMPTY customer key → the
// provider never added the device peer → the mobile WG handshake got no
// response → "Resolving peer" failed for every real session.
//
// The in-memory store kept the whole struct, so the unit-level
// mobile_session_test passed — only Postgres dropped the column. This
// test runs against real Postgres and asserts the key round-trips through
// CreateSession → ListAssignedSessions.
func TestPostgres_CreateSession_PersistsCustomerWgKey(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	providerID := uuid.New()
	seedProviderRow(t, ctx, p, providerID)

	const deviceKey = "l2bXKoVtjk8dg5tCImHH1qz3LasZr1pkm47I30tuLgE="
	expires := time.Now().Add(24 * time.Hour).UTC()
	sess := &Session{
		ID:                  uuid.New(),
		CustomerID:          uuid.New(),
		Region:              "us-east-1",
		PrimaryProvider:     providerID,
		CurrentProvider:     providerID,
		State:               pb.VpnSessionState_VPN_SESSION_STATE_CREATING,
		CreatedAt:           time.Now().UTC(),
		LastActivityAt:      time.Now().UTC(),
		CustomerWgPublicKey: deviceKey, // set by the mobile handler (#698)
		ClientPublicKey:     deviceKey,
		InnerIP:             "10.66.176.9/32",
		ExpiresAt:           &expires,
	}
	if err := p.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// The daemon's binder reads the customer key from /assigned-sessions.
	assigned, err := p.ListAssignedSessions(ctx, providerID)
	if err != nil {
		t.Fatalf("ListAssignedSessions: %v", err)
	}
	if len(assigned) != 1 {
		t.Fatalf("assigned-sessions len=%d, want 1", len(assigned))
	}
	if assigned[0].CustomerWgPublicKey != deviceKey {
		t.Fatalf("CustomerWgPublicKey=%q, want %q — the binder needs it to upsert the device peer; "+
			"an empty key here is the #701 'Resolving peer' bug",
			assigned[0].CustomerWgPublicKey, deviceKey)
	}
	// #701: the inner IP must also round-trip — the daemon's #695 return-
	// routing needs it. scanSession reads host(inner_ip) (mask stripped).
	if assigned[0].InnerIP != "10.66.176.9" {
		t.Fatalf("InnerIP=%q, want %q — needed for multi-customer egress routing",
			assigned[0].InnerIP, "10.66.176.9")
	}

	// GetSession must also see it (the customer-facing peer config read).
	got, err := p.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.CustomerWgPublicKey != deviceKey {
		t.Fatalf("GetSession CustomerWgPublicKey=%q, want %q", got.CustomerWgPublicKey, deviceKey)
	}
}
