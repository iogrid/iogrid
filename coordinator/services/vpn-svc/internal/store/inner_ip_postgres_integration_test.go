//go:build integration

// Postgres integration tests for the #605 mobile-session-bring-up store
// methods: AllocateInnerIP + PersistSessionPeerConfig.
//
// The Memory impl tests live alongside in inner_ip_test.go and exercise
// the same contract against the in-memory store. This file pins the
// Postgres path on a real database so we catch SQL-level drift
// (column names, ON CONFLICT semantics, RowsAffected behaviour) that
// the in-memory mirror can't surface.
//
// Run with:
//
//	docker run --rm -d -p 5432:5432 \
//	    -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=vpn_svc_test \
//	    postgres:16
//	DATABASE_URL=postgres://postgres:postgres@localhost:5432/vpn_svc_test?sslmode=disable \
//	    go test -tags=integration ./internal/store/...
//
// If DATABASE_URL is unset and the default localhost DSN is unreachable
// the tests skip cleanly — the same pattern used by
// coordinator/services/antiabuse-svc/internal/audit/retention_integration_test.go.
//
// Refs #605.

package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	// stdlib registers the "pgx" sql.Driver name that goose
	// (via vpndb.Apply) uses to open the migration DB handle.
	_ "github.com/jackc/pgx/v5/stdlib"

	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	vpndb "github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/db"
)

const defaultPostgresDSN = "postgres://postgres:postgres@localhost:5432/vpn_svc_test?sslmode=disable"

func postgresDSN() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultPostgresDSN
}

// newPostgresFixture brings up a pgxpool against the configured DATABASE_URL,
// drops any pre-existing schema, then re-applies all embedded migrations so
// every test starts from a clean baseline. Skips cleanly if the database is
// unreachable (CI without docker, dev box without a local Postgres, etc.).
func newPostgresFixture(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	dsn := postgresDSN()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("postgres pool create failed (DATABASE_URL=%q): %v", dsn, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres ping failed (DATABASE_URL=%q): %v", dsn, err)
	}

	// Wipe + re-migrate. We drop the goose marker table too so Apply
	// re-runs from scratch every test run — these tests are
	// destructive by design, so DATABASE_URL must point at a throwaway
	// database.
	wipeCtx, wipeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer wipeCancel()
	dropStmts := []string{
		`DROP TABLE IF EXISTS vpn_provider_inner_ip_alloc CASCADE`,
		`DROP TABLE IF EXISTS grid_payment_escrow CASCADE`,
		`DROP TABLE IF EXISTS ice_candidates CASCADE`,
		`DROP TABLE IF EXISTS vpn_providers CASCADE`,
		`DROP TABLE IF EXISTS vpn_sessions CASCADE`,
		`DROP TABLE IF EXISTS goose_db_version CASCADE`,
	}
	for _, s := range dropStmts {
		if _, err := pool.Exec(wipeCtx, s); err != nil {
			pool.Close()
			t.Fatalf("wipe (%s): %v", s, err)
		}
	}

	if err := vpndb.Apply(context.Background(), dsn); err != nil {
		pool.Close()
		t.Fatalf("apply migrations: %v", err)
	}

	cleanup := func() {
		pool.Close()
	}
	return pool, cleanup
}

// seedProviderRow inserts a row into vpn_providers so the FK-like
// integrity of the inner-IP allocator + session-update tests is
// realistic. There's no actual FK constraint on
// vpn_provider_inner_ip_alloc.provider_id, but seeding a provider row
// keeps the test data plausible if we ever add one.
func seedProviderRow(t *testing.T, ctx context.Context, p *Postgres, providerID uuid.UUID) {
	t.Helper()
	err := p.RegisterProvider(ctx, &ProviderInfo{
		ID:         providerID,
		Region:     "us-east-1",
		Status:     "healthy",
		LastSeenAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed provider %s: %v", providerID, err)
	}
}

// seedSessionRow inserts a vpn_sessions row owned by `customerID`
// pointing at `providerID`. Returns the session ID.
func seedSessionRow(t *testing.T, ctx context.Context, p *Postgres, providerID, customerID uuid.UUID) uuid.UUID {
	t.Helper()
	sessionID := uuid.New()
	err := p.CreateSession(ctx, &Session{
		ID:              sessionID,
		CustomerID:      customerID,
		Region:          "us-east-1",
		PrimaryProvider: providerID,
		CurrentProvider: providerID,
		State:           pb.VpnSessionState_VPN_SESSION_STATE_ACTIVE,
		CreatedAt:       time.Now().UTC(),
		LastActivityAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return sessionID
}

// TestPostgresAllocateInnerIP_ReturnsClampedXAndMonotonicY verifies the
// dotted-quad shape: X = providerID[0] clamped to [2, 253], Y monotonic
// starting at the next_suffix bump (2 on first call per provider).
func TestPostgresAllocateInnerIP_ReturnsClampedXAndMonotonicY(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	// providerID[0] = 42 → X octet = 42.
	providerID := uuid.UUID{42, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	seedProviderRow(t, ctx, p, providerID)

	customerID := uuid.New()
	session1 := seedSessionRow(t, ctx, p, providerID, customerID)
	session2 := seedSessionRow(t, ctx, p, providerID, customerID)

	ip1, err := p.AllocateInnerIP(ctx, providerID, session1)
	if err != nil {
		t.Fatalf("AllocateInnerIP 1: %v", err)
	}
	ip2, err := p.AllocateInnerIP(ctx, providerID, session2)
	if err != nil {
		t.Fatalf("AllocateInnerIP 2: %v", err)
	}

	// Shape check.
	if !strings.HasPrefix(ip1, "10.66.42.") {
		t.Errorf("ip1 = %q, want prefix 10.66.42.", ip1)
	}
	if !strings.HasPrefix(ip2, "10.66.42.") {
		t.Errorf("ip2 = %q, want prefix 10.66.42.", ip2)
	}
	if ip1 == ip2 {
		t.Fatalf("expected distinct IPs across (provider, different-session), got %s and %s", ip1, ip2)
	}

	// Y must start at 2 (next_suffix bootstraps at 1, first INSERT
	// branch sets it to 2 via the VALUES clause; ON CONFLICT bumps
	// further calls to 3, 4, …).
	if !strings.HasSuffix(ip1, ".2") {
		t.Errorf("first allocation should land on .2, got %s", ip1)
	}
	if !strings.HasSuffix(ip2, ".3") {
		t.Errorf("second allocation should land on .3, got %s", ip2)
	}
}

// TestPostgresAllocateInnerIP_ClampsXOctet exercises the X clamping
// for providers whose UUID first byte falls below 2 or above 253. The
// memory impl clamps to [2, 253]; the Postgres impl mirrors that.
func TestPostgresAllocateInnerIP_ClampsXOctet(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	// providerID[0] = 0 → must clamp up to 2.
	lowProvider := uuid.UUID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	seedProviderRow(t, ctx, p, lowProvider)
	lowSession := seedSessionRow(t, ctx, p, lowProvider, uuid.New())
	lowIP, err := p.AllocateInnerIP(ctx, lowProvider, lowSession)
	if err != nil {
		t.Fatalf("low: %v", err)
	}
	if !strings.HasPrefix(lowIP, "10.66.2.") {
		t.Errorf("low provider should clamp to X=2, got %s", lowIP)
	}

	// providerID[0] = 255 → must clamp down to 253.
	highProvider := uuid.UUID{255, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}
	seedProviderRow(t, ctx, p, highProvider)
	highSession := seedSessionRow(t, ctx, p, highProvider, uuid.New())
	highIP, err := p.AllocateInnerIP(ctx, highProvider, highSession)
	if err != nil {
		t.Fatalf("high: %v", err)
	}
	if !strings.HasPrefix(highIP, "10.66.253.") {
		t.Errorf("high provider should clamp to X=253, got %s", highIP)
	}
}

// TestPostgresAllocateInnerIP_IdempotentPerSession verifies that a
// second call with the same (providerID, sessionID) returns the same
// IP rather than burning a new Y suffix. This requires the session
// row's inner_ip column to be set after the first allocation so the
// SELECT short-circuit in the Postgres impl fires.
func TestPostgresAllocateInnerIP_IdempotentPerSession(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	providerID := uuid.UUID{77, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	seedProviderRow(t, ctx, p, providerID)
	sessionID := seedSessionRow(t, ctx, p, providerID, uuid.New())

	ip1, err := p.AllocateInnerIP(ctx, providerID, sessionID)
	if err != nil {
		t.Fatalf("AllocateInnerIP 1: %v", err)
	}
	// Persist the allocation onto the session row so the
	// idempotency-check SELECT in the Postgres impl
	// (`SELECT host(inner_ip) FROM vpn_sessions WHERE id = $1`)
	// returns a non-null row on the second call. In production the
	// mobile handler stamps inner_ip onto vpn_sessions after a
	// successful allocation; the test simulates that step.
	if _, err := pool.Exec(ctx,
		`UPDATE vpn_sessions SET inner_ip = $1::inet WHERE id = $2`,
		ip1, sessionID); err != nil {
		t.Fatalf("stamp inner_ip: %v", err)
	}

	ip2, err := p.AllocateInnerIP(ctx, providerID, sessionID)
	if err != nil {
		t.Fatalf("AllocateInnerIP 2: %v", err)
	}
	if ip1 != ip2 {
		t.Errorf("AllocateInnerIP must be idempotent for same (provider, session): got %s then %s", ip1, ip2)
	}

	// And the next_suffix counter must NOT have been advanced by the
	// second call — only the first allocation should have bumped it.
	var nextSuffix int
	if err := pool.QueryRow(ctx,
		`SELECT next_suffix FROM vpn_provider_inner_ip_alloc WHERE provider_id = $1`,
		providerID).Scan(&nextSuffix); err != nil {
		t.Fatalf("query next_suffix: %v", err)
	}
	if nextSuffix != 2 {
		t.Errorf("idempotent call should not bump next_suffix, got %d", nextSuffix)
	}
}

// TestPostgresPersistSessionPeerConfig_WritesProviderWgPublicKey
// verifies the happy path: the UPDATE lands on vpn_sessions and a
// subsequent GetSession surfaces the persisted key.
func TestPostgresPersistSessionPeerConfig_WritesProviderWgPublicKey(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	providerID := uuid.New()
	seedProviderRow(t, ctx, p, providerID)
	sessionID := seedSessionRow(t, ctx, p, providerID, uuid.New())

	const peerPubKey = "AAAA1111BBBB2222CCCC3333DDDD4444EEEE5555FFFF6666GGGG7777="
	const peerEndpoint = "203.0.113.42:51820"
	if err := p.PersistSessionPeerConfig(ctx, sessionID, peerPubKey, peerEndpoint); err != nil {
		t.Fatalf("PersistSessionPeerConfig: %v", err)
	}

	got, err := p.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ProviderWgPublicKey != peerPubKey {
		t.Errorf("provider_wg_public_key = %q, want %q", got.ProviderWgPublicKey, peerPubKey)
	}
}

// TestPostgresPersistSessionPeerConfig_NotFound verifies the Postgres
// impl surfaces a "session %s not found" error when the UPDATE
// matches zero rows. The handler relies on this so it can return
// NotFound to the mobile client instead of swallowing the bad ID.
func TestPostgresPersistSessionPeerConfig_NotFound(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	missing := uuid.New()
	err := p.PersistSessionPeerConfig(ctx, missing, "key=", "1.2.3.4:5678")
	if err == nil {
		t.Fatal("expected error for unknown session id, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got %v", err)
	}
}
