//go:build integration

// Field-complete round-trip tests for the identity-svc auth-token stores the
// #726 audit verified CLEAN by inspection but left without a regression
// guard: sessions (server-side refresh tokens + step-up state) and
// magic_link_tokens (the emailed sign-in links).
//
// The bug class (#709 / #725 / #732): the in-memory impl keeps the whole
// struct, so unit tests are green, while the Postgres INSERT/SELECT silently
// drops a column — lost only in prod. These tests write a row populating
// EVERY caller-set field, read it back through the real getter the handlers
// use against a REAL Postgres, and assert each field survives. A future
// column added to store.Session / store.MagicLinkToken but not to the SQL
// fails here.
//
// Fixture: prefer an external DATABASE_URL (local podman dev — same as the
// billing-svc money_roundtrip suite), else spin up a one-shot Postgres via
// ory/dockertest so the test actually RUNS in identity-svc-integration.yml
// (which executes `go test -tags=integration ./...` with no DATABASE_URL and
// relies on dockertest, exactly like internal/auth's pgFixture). Either way:
// wipe + re-apply the embedded identity migrations per run; destructive by
// design.
package store_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	idb "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/db"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

func newAuthFixture(t *testing.T) (*store.Store, func()) {
	t.Helper()
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return newAuthFixtureFromDSN(t, dsn)
	}
	return newAuthFixtureDockertest(t)
}

// newAuthFixtureFromDSN wires the store to an already-running Postgres. Wipes
// the schema + re-applies migrations so each run starts clean.
func newAuthFixtureFromDSN(t *testing.T, dsn string) (*store.Store, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("pg pool create failed (DATABASE_URL=%q): %v", dsn, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("pg ping failed (DATABASE_URL=%q): %v", dsn, err)
	}
	wipeCtx, wipeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer wipeCancel()
	if _, err := pool.Exec(wipeCtx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		pool.Close()
		t.Fatalf("wipe schema: %v", err)
	}
	if err := idb.Apply(context.Background(), dsn); err != nil {
		pool.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	return store.New(pool), func() { pool.Close() }
}

// newAuthFixtureDockertest brings up a one-shot Postgres container (mirrors
// internal/auth/integration_test.go's pgFixture) so the suite runs in CI.
func newAuthFixtureDockertest(t *testing.T) (*store.Store, func()) {
	t.Helper()
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Skipf("dockertest pool unavailable: %v", err)
	}
	if err := pool.Client.Ping(); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_DB=identity",
			"listen_addresses='*'",
		},
	}, func(cfg *docker.HostConfig) {
		cfg.AutoRemove = true
		cfg.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		t.Fatalf("docker run postgres: %v", err)
	}
	_ = resource.Expire(120)

	dsn := fmt.Sprintf("postgres://postgres:secret@%s/identity?sslmode=disable", resource.GetHostPort("5432/tcp"))
	var pgxPool *pgxpool.Pool
	if err := pool.Retry(func() error {
		p, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			return err
		}
		if err := p.Ping(context.Background()); err != nil {
			p.Close()
			return err
		}
		pgxPool = p
		return nil
	}); err != nil {
		t.Fatalf("postgres ready: %v", err)
	}
	if err := idb.Apply(context.Background(), dsn); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return store.New(pgxPool), func() {
		pgxPool.Close()
		_ = pool.Purge(resource)
	}
}

// seedAuthUser inserts the FK user that sessions / magic-link tokens point at.
func seedAuthUser(t *testing.T, st *store.Store, email string) *store.User {
	t.Helper()
	u := &store.User{
		ID:           uuid.New(),
		PrimaryEmail: email,
		DisplayName:  "RT",
		Roles:        []string{"customer"},
	}
	if err := st.CreateUser(context.Background(), nil, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

// TestSession_RoundTrip locks the caller-set fields of a sessions row through
// CreateSession → FindSessionByRefreshHash (and the list path), then exercises
// the two state-mutating UPDATE paths (MarkSessionStepUp, RevokeSession). A
// dropped column here (e.g. ip, user_agent, step_up_until) would silently
// weaken the security-sensitive session record.
func TestSession_RoundTrip(t *testing.T) {
	st, cleanup := newAuthFixture(t)
	defer cleanup()
	ctx := context.Background()

	u := seedAuthUser(t, st, "session-rt@example.com")

	expires := time.Now().UTC().Add(720 * time.Hour).Truncate(time.Microsecond)
	want := &store.Session{
		UserID:           u.ID,
		RefreshTokenHash: "rt-hash-roundtrip-deadbeef0001",
		IP:               net.ParseIP("203.0.113.42"),
		UserAgent:        "iogrid-roundtrip/1.0 (test)",
		ExpiresAt:        expires,
	}
	if err := st.CreateSession(ctx, nil, want); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if want.ID == uuid.Nil {
		t.Fatal("CreateSession did not stamp ID")
	}
	if want.CreatedAt.IsZero() || want.LastUsedAt.IsZero() {
		t.Fatal("CreateSession did not stamp created_at / last_used_at")
	}

	got, err := st.FindSessionByRefreshHash(ctx, nil, want.RefreshTokenHash)
	if err != nil {
		t.Fatalf("FindSessionByRefreshHash: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.UserID != want.UserID {
		t.Errorf("UserID = %v, want %v", got.UserID, want.UserID)
	}
	if got.RefreshTokenHash != want.RefreshTokenHash {
		t.Errorf("RefreshTokenHash = %q, want %q", got.RefreshTokenHash, want.RefreshTokenHash)
	}
	// The INET column round-trips via host(ip) — the original ip::text read
	// appended "/32", which net.ParseIP rejects (nil), silently dropping the
	// IP. This assertion is the regression guard for that fix (#726).
	if got.IP == nil || !got.IP.Equal(want.IP) {
		t.Errorf("IP = %v, want %v — the ip column was dropped (ip::text /32 → ParseIP nil)", got.IP, want.IP)
	}
	if got.UserAgent != want.UserAgent {
		t.Errorf("UserAgent = %q, want %q", got.UserAgent, want.UserAgent)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
	if got.RevokedAt != nil {
		t.Errorf("fresh session RevokedAt = %v, want nil", got.RevokedAt)
	}
	if got.StepUpUntil != nil {
		t.Errorf("fresh session StepUpUntil = %v, want nil", got.StepUpUntil)
	}

	// The list path the /account "active sessions" surface uses must carry
	// the same fields.
	list, err := st.ListSessionsForUser(ctx, nil, u.ID)
	if err != nil {
		t.Fatalf("ListSessionsForUser: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListSessionsForUser len = %d, want 1", len(list))
	}
	if list[0].IP == nil || !list[0].IP.Equal(want.IP) || list[0].UserAgent != want.UserAgent {
		t.Errorf("list row dropped ip/user_agent: ip=%v ua=%q", list[0].IP, list[0].UserAgent)
	}

	// step_up_until must survive its dedicated UPDATE round-trip (the
	// fresh-auth window for payout / merge / delete).
	stepUp := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Microsecond)
	if err := st.MarkSessionStepUp(ctx, nil, want.ID, stepUp); err != nil {
		t.Fatalf("MarkSessionStepUp: %v", err)
	}
	got2, err := st.FindSessionByRefreshHash(ctx, nil, want.RefreshTokenHash)
	if err != nil {
		t.Fatalf("Find after step-up: %v", err)
	}
	if got2.StepUpUntil == nil || !got2.StepUpUntil.Equal(stepUp) {
		t.Errorf("StepUpUntil = %v, want %v", got2.StepUpUntil, stepUp)
	}

	// revoked_at: after RevokeSession the live-session getter must no longer
	// return it (revoked_at IS NULL filter), proving the column is honored.
	if err := st.RevokeSession(ctx, nil, want.ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := st.FindSessionByRefreshHash(ctx, nil, want.RefreshTokenHash); err == nil {
		t.Error("revoked session still returned by FindSessionByRefreshHash — revoked_at not persisted/honored")
	}
}

// TestMagicLinkToken_RoundTrip locks every caller-set field of a
// magic_link_tokens row through CreateMagicLinkToken → ConsumeMagicLinkToken.
// The token is the sign-in credential — a dropped intent / user_id / return_to
// silently breaks the merge / step-up / post-login redirect flows.
func TestMagicLinkToken_RoundTrip(t *testing.T) {
	st, cleanup := newAuthFixture(t)
	defer cleanup()
	ctx := context.Background()

	// A merge-intent token carries a user_id (the account to attach the
	// email to); exercise the non-nil pointer path.
	u := seedAuthUser(t, st, "magic-rt@example.com")

	expires := time.Now().UTC().Add(15 * time.Minute).Truncate(time.Microsecond)
	want := &store.MagicLinkToken{
		TokenHash: "ml-hash-roundtrip-cafe0001",
		Email:     "magic-rt@example.com",
		Intent:    store.IntentMerge,
		UserID:    &u.ID,
		ReturnTo:  "/account?merged=1",
		ExpiresAt: expires,
	}
	if err := st.CreateMagicLinkToken(ctx, nil, want); err != nil {
		t.Fatalf("CreateMagicLinkToken: %v", err)
	}
	if want.CreatedAt.IsZero() {
		t.Fatal("CreateMagicLinkToken did not stamp created_at")
	}

	// ConsumeMagicLinkToken is the only full-row read path; it returns every
	// column. (It also marks used_at — asserted below.)
	got, err := st.ConsumeMagicLinkToken(ctx, nil, want.TokenHash)
	if err != nil {
		t.Fatalf("ConsumeMagicLinkToken: %v", err)
	}
	if got.TokenHash != want.TokenHash {
		t.Errorf("TokenHash = %q, want %q", got.TokenHash, want.TokenHash)
	}
	if got.Email != want.Email {
		t.Errorf("Email = %q, want %q", got.Email, want.Email)
	}
	if got.Intent != want.Intent {
		t.Errorf("Intent = %q, want %q — the intent column was dropped", got.Intent, want.Intent)
	}
	if got.UserID == nil || *got.UserID != *want.UserID {
		t.Errorf("UserID = %v, want %v — the nullable user_id was dropped", got.UserID, want.UserID)
	}
	if got.ReturnTo != want.ReturnTo {
		t.Errorf("ReturnTo = %q, want %q — the post-login redirect was dropped", got.ReturnTo, want.ReturnTo)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if got.UsedAt == nil {
		t.Error("UsedAt nil after Consume — used_at not stamped/persisted (replay-protection bug)")
	}

	// Second consume must fail (used_at IS NULL filter) — proves the used_at
	// write landed and is honored on re-read.
	if _, err := st.ConsumeMagicLinkToken(ctx, nil, want.TokenHash); err == nil {
		t.Error("magic-link token consumed twice — used_at not persisted/honored")
	}

	// A signin-intent token with NO user_id must round-trip the NULL path.
	bare := &store.MagicLinkToken{
		TokenHash: "ml-hash-roundtrip-cafe0002",
		Email:     "fresh-signup@example.com",
		Intent:    store.IntentSignIn,
		ReturnTo:  "",
		ExpiresAt: expires,
	}
	if err := st.CreateMagicLinkToken(ctx, nil, bare); err != nil {
		t.Fatalf("CreateMagicLinkToken (no user_id): %v", err)
	}
	gotBare, err := st.ConsumeMagicLinkToken(ctx, nil, bare.TokenHash)
	if err != nil {
		t.Fatalf("ConsumeMagicLinkToken (no user_id): %v", err)
	}
	if gotBare.UserID != nil {
		t.Errorf("signin token UserID = %v, want nil (NULL path)", gotBare.UserID)
	}
	if gotBare.Intent != store.IntentSignIn {
		t.Errorf("Intent = %q, want %q", gotBare.Intent, store.IntentSignIn)
	}
}
