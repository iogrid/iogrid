//go:build integration
// +build integration

// Concurrent CompleteAppleSignIn race regression — #614.
//
// The Apple sign-in flow's race-recovery branch (apple.go:106) catches a
// unique-violation on the apple_sub_hash INSERT and falls back to a
// re-read so the loser of a first-launch race still gets back the same
// user as the winner. This file exercises that branch under realistic
// concurrency: 16 goroutines each call CompleteAppleSignIn with the same
// validated Apple sub.
//
// Acceptance:
//   - all 16 callers return success (no error surfaces to the client)
//   - exactly one users row carries the apple_sub_hash
//   - every caller's returned User.ID matches that one row
//
// CURRENT STATE: this test FAILS on main as of 2026-06-03 because the
// race-recovery branch in apple.go:106 only handles SQLSTATE 23505
// (unique violation). Under the Serializable isolation level that
// store.WithTx uses, concurrent INSERTs surface SQLSTATE 40001
// (serialization failure) instead, which apple.go propagates to the
// caller. Tracked as #620 — leave this test as the regression repro;
// it goes GREEN once #620 lands.
//
// The fixture stands up postgres:16 on port 55434 (offset from the
// vpn-svc inner-IP integration suite's 5432 default and from the
// identity-svc dockertest fixtures which use random high ports) and
// SKIPs cleanly when DATABASE_URL is unset and the default DSN isn't
// reachable — same pattern as
// coordinator/services/vpn-svc/internal/store/inner_ip_postgres_integration_test.go.
//
// Run with:
//
//	docker run --rm -d -p 55434:5432 \
//	    -e POSTGRES_PASSWORD=secret -e POSTGRES_DB=identity \
//	    postgres:16
//	DATABASE_URL=postgres://postgres:secret@localhost:55434/identity?sslmode=disable \
//	    go test -tags=integration -run TestApple_ConcurrentFirstLaunch \
//	    ./coordinator/services/identity-svc/internal/auth/...
//
// Refs #614, #620.

package auth

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"crypto/rand"
	"crypto/rsa"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	idb "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/db"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/mail"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/ratelimit"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

// defaultConcurrentDSN points at a sidecar postgres:16 on port 55434 so
// this suite can run alongside the vpn-svc integration tests (which
// default to port 5432) without clashing. CI / dev should publish
// 55434:5432 on a throwaway postgres container before invoking
// `go test -tags=integration`.
const defaultConcurrentDSN = "postgres://postgres:secret@localhost:55434/identity?sslmode=disable"

func concurrentPostgresDSN() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultConcurrentDSN
}

// concurrentPGFixture brings up a pgxpool against the configured DSN,
// wipes any pre-existing identity-svc schema, then re-applies the
// embedded migrations so each test starts clean. Skips when the
// database isn't reachable (CI without docker, dev box without a
// postgres on :55434, etc).
func concurrentPGFixture(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	dsn := concurrentPostgresDSN()

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

	// Wipe + re-migrate. Reset the identity-svc tables and the goose
	// bookkeeping so Apply runs from scratch every test run. DATABASE_URL
	// MUST point at a throwaway database — this is destructive.
	wipeCtx, wipeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer wipeCancel()
	dropStmts := []string{
		`DROP SCHEMA public CASCADE`,
		`CREATE SCHEMA public`,
	}
	for _, s := range dropStmts {
		if _, err := pool.Exec(wipeCtx, s); err != nil {
			pool.Close()
			t.Fatalf("wipe (%s): %v", s, err)
		}
	}

	if err := idb.Apply(context.Background(), dsn); err != nil {
		pool.Close()
		t.Fatalf("apply migrations: %v", err)
	}

	cleanup := func() {
		pool.Close()
	}
	return pool, cleanup
}

// newConcurrentTestService wires a minimal auth.Service against the
// concurrent fixture's Postgres pool plus an in-memory mail sender +
// in-memory limiter. The signer uses a freshly minted RSA key so the
// bundle issuance code path is exercised end-to-end (it's part of the
// CompleteAppleSignIn return shape — callers check Bundle != nil).
func newConcurrentTestService(t *testing.T, pool *pgxpool.Pool) *Service {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	signer := tokens.NewSignerFromKeys(priv, "test", "https://test.iogrid.org", []string{"x"}, 15*time.Minute)
	return New(Options{
		Store:              store.New(pool),
		Mail:               &mail.MemorySender{},
		Signer:             signer,
		Limiter:            ratelimit.NewMemory(),
		Logger:             slog.New(slog.NewTextHandler(os.Stderr, nil)),
		BaseURL:            "http://localhost:8080",
		AllowedReturnHosts: []string{"iogrid.org", "localhost"},
		MagicLinkTTL:       10 * time.Minute,
		RefreshTokenTTL:    30 * 24 * time.Hour,
	})
}

// TestApple_ConcurrentFirstLaunch_OneUserCreated fires 16 concurrent
// goroutines that all call CompleteAppleSignIn with the same Apple sub.
// Only ONE row should land in `users` (the unique partial index on
// users.apple_sub_hash guards that invariant); the other 15 callers
// must succeed by hitting the race-recovery re-read branch and return
// the same user ID as the winner.
//
// This is the regression test for the #614 acceptance bullets:
//   - Exactly 1 row in users with the apple_sub_hash
//   - All 16 goroutines received the same user_id in their AuthBundle
//   - No errors returned to any caller
func TestApple_ConcurrentFirstLaunch_OneUserCreated(t *testing.T) {
	pool, cleanup := concurrentPGFixture(t)
	defer cleanup()
	svc := newConcurrentTestService(t, pool)

	// Stand up the fake JWKS server + AppleValidator. fakeJWKSServer +
	// freshClaims live in apple_token_validator_test.go which is in the
	// same package, so the helpers are reachable from this integration
	// file under the same -tags=integration build.
	f := newFakeJWKSServer(t)
	cache := NewJWKSCache(f.url(), 24*time.Hour, http.DefaultClient)
	svc.Apple = NewAppleValidator(cache)
	svc.AppleSubSalt = []byte("concurrent-test-salt")

	const (
		appleSub = "apple-sub-concurrent-#614"
		nonce    = "client-nonce-#614"
	)
	claims := freshClaims(appleSub, AppleAudience, AppleIssuer, nonce, time.Now().Add(5*time.Minute))
	claims.Email = "race@example.com"
	token := f.sign(t, "", claims)

	const N = 16
	type outcome struct {
		userID uuid.UUID
		err    error
		isNew  bool
	}
	out := make([]outcome, N)

	// Use a barrier so all 16 goroutines start the CompleteAppleSignIn
	// call as close to simultaneously as possible — maximises the
	// probability that >1 transaction enters the INSERT branch
	// concurrently and exercises the race-recovery code path.
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			<-start
			res, err := svc.CompleteAppleSignIn(context.Background(), token, nonce, "Race Tester", req)
			if err != nil {
				out[i] = outcome{err: err}
				return
			}
			out[i] = outcome{
				userID: res.Bundle.User.ID,
				isNew:  res.NewUser,
			}
		}()
	}
	close(start)
	wg.Wait()

	// 1. No errors surfaced to any caller.
	var firstErr error
	errCount := 0
	for i, o := range out {
		if o.err != nil {
			errCount++
			if firstErr == nil {
				firstErr = o.err
			}
			t.Errorf("goroutine %d returned error: %v", i, o.err)
		}
	}
	if errCount > 0 {
		// Surface the count first so the failure mode is obvious in CI
		// logs even if the per-goroutine asserts above are truncated.
		t.Fatalf("%d/%d concurrent CompleteAppleSignIn calls failed; first error: %v", errCount, N, firstErr)
	}

	// 2. Exactly 1 row in users with the apple_sub_hash matching the
	//    Apple sub. We compute the hash with the same salt the service
	//    used.
	hash := hashAppleSub(appleSub, svc.AppleSubSalt)
	var rowCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM users WHERE apple_sub_hash = $1`, hash).Scan(&rowCount); err != nil {
		t.Fatalf("count users with apple_sub_hash: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("expected exactly 1 user row with apple_sub_hash, got %d", rowCount)
	}

	// 3. All goroutines must agree on the same user_id (whichever
	//    transaction won the race, every other caller must have
	//    re-read the same row).
	first := out[0].userID
	if first == uuid.Nil {
		t.Fatalf("goroutine 0 returned zero user id; cannot proceed")
	}
	mismatched := 0
	for i, o := range out {
		if o.userID != first {
			mismatched++
			t.Errorf("goroutine %d returned user id %s, expected %s", i, o.userID, first)
		}
	}
	if mismatched > 0 {
		t.Fatalf("%d/%d concurrent CompleteAppleSignIn calls returned a divergent user id", mismatched, N)
	}

	// 4. And the surviving user_id must match the actual users row.
	var dbUserID uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE apple_sub_hash = $1`, hash).Scan(&dbUserID); err != nil {
		t.Fatalf("read user id by apple_sub_hash: %v", err)
	}
	if dbUserID != first {
		t.Errorf("DB user id %s != reported user id %s", dbUserID, first)
	}

	// 5. Exactly one goroutine should have reported NewUser=true — the
	//    one that won the INSERT race. The other 15 hit either the
	//    fast-path (FindUserByAppleSubHash succeeds after the winner
	//    commits) or the race-recovery re-read (FindUserByAppleSubHash
	//    after the unique-violation). Both report NewUser=false. We
	//    don't HARD-fail on this — depending on commit timing it's
	//    possible for the fixture to land "0 NewUser=true" if the
	//    winner reports false via the fast-path retry semantics in
	//    some future refactor; that's a softer assertion captured as
	//    a logged warning so the test stays robust to the apple.go
	//    refactor surface.
	newCount := 0
	for _, o := range out {
		if o.isNew {
			newCount++
		}
	}
	if newCount > 1 {
		t.Errorf("more than one goroutine reported NewUser=true (%d); race-recovery branch must report NewUser=false", newCount)
	}
	if newCount == 0 {
		t.Logf("note: no goroutine reported NewUser=true — winning INSERT must always set NewUser=true; check apple.go race-recovery branch")
	}
}
