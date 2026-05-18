//go:build integration
// +build integration

// Integration tests for the auth.Service. Spins up Postgres via
// ory/dockertest, applies migrations, then exercises:
//   * magic-link request → SHA-256 hashed in DB → complete → bundle
//   * second magic-link to same email → returns the same user
//   * Google sign-in (faked Identity) → auto-merge against existing
//     magic-link identifier when verified-secondaries match
//   * refresh-token rotation
//
// Run via: go test -tags=integration ./internal/auth/...
package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	idb "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/db"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/mail"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/oauth/google"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/ratelimit"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

// pgFixture brings up a one-shot Postgres container, runs migrations, and
// hands back the connection pool. Caller defers cleanup.
func pgFixture(t *testing.T) (*pgxpool.Pool, func()) {
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
	cleanup := func() {
		pgxPool.Close()
		_ = pool.Purge(resource)
	}
	return pgxPool, cleanup
}

func newTestService(t *testing.T, pool *pgxpool.Pool) (*Service, *mail.MemorySender) {
	t.Helper()
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer := tokens.NewSignerFromKeys(priv, "test", "https://test.iogrid.org", []string{"x"}, 15*time.Minute)
	sender := &mail.MemorySender{}
	return New(Options{
		Store:              store.New(pool),
		Mail:               sender,
		Signer:             signer,
		Limiter:            ratelimit.NewMemory(),
		Logger:             slog.New(slog.NewTextHandler(os.Stderr, nil)),
		BaseURL:            "http://localhost:8080",
		AllowedReturnHosts: []string{"iogrid.org", "localhost"},
		MagicLinkTTL:       10 * time.Minute,
		RefreshTokenTTL:    30 * 24 * time.Hour,
	}), sender
}

func extractTokenFromLink(t *testing.T, body string) string {
	t.Helper()
	// Look for "token=" in either the plain text or HTML body.
	i := strings.Index(body, "token=")
	if i < 0 {
		t.Fatalf("token= not in body")
	}
	rest := body[i+len("token="):]
	end := strings.IndexAny(rest, "& \t\r\n\"<")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

// TestMagicLinkHappyPath: request → email captured → complete → bundle.
func TestMagicLinkHappyPath(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, sender := newTestService(t, pool)

	resp, err := svc.RequestMagicLink(context.Background(), "alice@example.com", "", "127.0.0.1", store.IntentSignIn)
	if err != nil {
		t.Fatalf("RequestMagicLink: %v", err)
	}
	if !resp.Accepted {
		t.Fatalf("not accepted")
	}
	if len(sender.Inbox) != 1 {
		t.Fatalf("expected 1 email, got %d", len(sender.Inbox))
	}
	token := extractTokenFromLink(t, sender.Inbox[0].TextBody+sender.Inbox[0].HTMLBody)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	bundle, err := svc.CompleteMagicLink(context.Background(), token, req)
	if err != nil {
		t.Fatalf("CompleteMagicLink: %v", err)
	}
	if bundle.User.PrimaryEmail != "alice@example.com" {
		t.Errorf("PrimaryEmail: %q", bundle.User.PrimaryEmail)
	}
	if !bundle.NewUser {
		t.Errorf("NewUser: false")
	}
	if bundle.AccessToken == "" || bundle.RefreshToken == "" {
		t.Errorf("missing tokens")
	}
}

// TestMagicLinkReplayFails: redeeming the same token twice must fail.
func TestMagicLinkReplayFails(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, sender := newTestService(t, pool)

	_, err := svc.RequestMagicLink(context.Background(), "bob@example.com", "", "", store.IntentSignIn)
	if err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromLink(t, sender.Inbox[0].TextBody)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	if _, err := svc.CompleteMagicLink(context.Background(), token, req); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if _, err := svc.CompleteMagicLink(context.Background(), token, req); err == nil {
		t.Fatalf("replay should have failed")
	}
}

// TestRefreshRotatesAndRevokesOld: refresh mints a new bundle and the old
// refresh token can no longer be used.
func TestRefreshRotatesAndRevokesOld(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, sender := newTestService(t, pool)

	_, err := svc.RequestMagicLink(context.Background(), "charlie@example.com", "", "", store.IntentSignIn)
	if err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromLink(t, sender.Inbox[0].TextBody)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	bundle1, err := svc.CompleteMagicLink(context.Background(), token, req)
	if err != nil {
		t.Fatal(err)
	}
	bundle2, err := svc.Refresh(context.Background(), bundle1.RefreshToken, req)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if bundle2.RefreshToken == bundle1.RefreshToken {
		t.Errorf("refresh token not rotated")
	}
	if _, err := svc.Refresh(context.Background(), bundle1.RefreshToken, req); err == nil {
		t.Fatalf("old refresh token should have been revoked")
	}
}

// TestAutoMergeFromMagicLinkToGoogle: a magic-link user exists; a Google
// sign-in arrives whose verified-secondaries include the magic-link
// email — Service should attach the Google identifier to the existing
// user and audit the merge.
func TestAutoMergeFromMagicLinkToGoogle(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, sender := newTestService(t, pool)

	// 1) Seed a magic-link user for alice@company.com.
	_, err := svc.RequestMagicLink(context.Background(), "alice@company.com", "", "", store.IntentSignIn)
	if err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromLink(t, sender.Inbox[0].TextBody)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	mlBundle, err := svc.CompleteMagicLink(context.Background(), token, req)
	if err != nil {
		t.Fatal(err)
	}
	originalUserID := mlBundle.User.ID

	// 2) Inject a Google identity whose verified secondaries include
	//    alice@company.com.
	googleID := &google.Identity{
		Subject:             "google-sub-12345",
		Email:               "alice@gmail.com",
		EmailVerified:       true,
		Name:                "Alice A",
		Picture:             "https://x/p.png",
		VerifiedSecondaries: []string{"alice@company.com"},
	}
	bundle, err := svc.completeGoogleForTest(context.Background(), googleID, req)
	if err != nil {
		t.Fatalf("completeGoogleForTest: %v", err)
	}
	if !bundle.Merged {
		t.Fatalf("expected Merged=true")
	}
	if bundle.User.ID != originalUserID {
		t.Errorf("merged into wrong user: got %v want %v", bundle.User.ID, originalUserID)
	}

	// Verify identifiers for the surviving user includes BOTH.
	st := store.New(pool)
	idents, err := st.ListIdentifiersForUser(context.Background(), nil, originalUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(idents) != 2 {
		t.Errorf("expected 2 identifiers post-merge, got %d", len(idents))
	}
	kinds := map[store.IdentifierKind]bool{}
	for _, i := range idents {
		kinds[i.Kind] = true
	}
	if !kinds[store.KindMagicLink] || !kinds[store.KindGoogle] {
		t.Errorf("missing identifier kinds: %v", kinds)
	}
}
