//go:build integration
// +build integration

// SIWS-specific integration tests. Reuses the same dockertest-backed
// Postgres fixture as integration_test.go and exercises the wallet-bind
// flow end-to-end:
//
//   * happy path — challenge + sign + verify + identifier row appears
//   * replay attack — second Complete with the same signature is rejected
//   * unbind — DeleteIdentifier path; the wallet can no longer auth
//   * auto-bind on first Solana auth — create_if_missing mints a fresh
//     User and returns a sign-in bundle
//   * JWT claims include solana_addresses after binding
//
// Run via: go test -tags=integration ./internal/auth/...
package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/siws"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// freshWallet returns an ed25519 keypair + base58 address that mimics a
// Phantom-connected wallet.
func freshWallet(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	return priv, siws.EncodeAddress(pub)
}

// signChallenge signs the bytes the server returned and base58-encodes
// the result, the way Phantom's signMessage RPC returns it.
func signChallenge(priv ed25519.PrivateKey, challenge string) string {
	return siws.EncodeSignature(ed25519.Sign(priv, []byte(challenge)))
}

// TestSiwsHappyPath: existing user binds a wallet → identifier row exists
// → JWT claim now carries the address.
func TestSiwsHappyPath(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, sender := newTestService(t, pool)

	// Seed a magic-link user.
	_, err := svc.RequestMagicLink(context.Background(), "alice@example.com", "", "", store.IntentSignIn)
	if err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromLink(t, sender.Inbox[0].TextBody)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	bundle, err := svc.CompleteMagicLink(context.Background(), token, req)
	if err != nil {
		t.Fatal(err)
	}
	userID := bundle.User.ID

	// Start SIWS.
	priv, addr := freshWallet(t)
	start, err := svc.StartSiwsBinding(context.Background(), userID, addr)
	if err != nil {
		t.Fatalf("StartSiwsBinding: %v", err)
	}
	if start.Challenge == "" {
		t.Fatalf("empty challenge")
	}
	sig := signChallenge(priv, start.Challenge)

	// Complete.
	res, err := svc.CompleteSiwsBinding(context.Background(), userID, addr, sig, false, req)
	if err != nil {
		t.Fatalf("CompleteSiwsBinding: %v", err)
	}
	if res.UserID != userID {
		t.Errorf("UserID mismatch: %v vs %v", res.UserID, userID)
	}
	if res.NewUser {
		t.Errorf("NewUser=true on existing user")
	}

	// Bound wallet is visible.
	bindings, err := svc.ListBoundWallets(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListBoundWallets: %v", err)
	}
	if len(bindings) != 1 || bindings[0].Subject != addr {
		t.Fatalf("bindings: %+v", bindings)
	}

	// JWT claim now carries the address.
	refreshed, err := svc.Refresh(context.Background(), bundle.RefreshToken, req)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	claims, err := svc.Signer.Verify(refreshed.AccessToken)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(claims.SolanaAddresses) != 1 || claims.SolanaAddresses[0] != addr {
		t.Fatalf("claims.SolanaAddresses = %v", claims.SolanaAddresses)
	}
}

// TestSiwsReplayBlocked: a second Complete with the same signature must
// fail because the challenge nonce is consumed on the first call.
func TestSiwsReplayBlocked(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, sender := newTestService(t, pool)

	if _, err := svc.RequestMagicLink(context.Background(), "replay@example.com", "", "", store.IntentSignIn); err != nil {
		t.Fatal(err)
	}
	token := extractTokenFromLink(t, sender.Inbox[0].TextBody)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	bundle, err := svc.CompleteMagicLink(context.Background(), token, req)
	if err != nil {
		t.Fatal(err)
	}
	userID := bundle.User.ID

	priv, addr := freshWallet(t)
	start, err := svc.StartSiwsBinding(context.Background(), userID, addr)
	if err != nil {
		t.Fatal(err)
	}
	sig := signChallenge(priv, start.Challenge)
	if _, err := svc.CompleteSiwsBinding(context.Background(), userID, addr, sig, false, req); err != nil {
		t.Fatalf("first complete: %v", err)
	}
	// Replay — the challenge has been consumed.
	if _, err := svc.CompleteSiwsBinding(context.Background(), userID, addr, sig, false, req); err == nil {
		t.Fatalf("replay should have failed")
	}
}

// TestSiwsUnbindThenAuthFails: after Unbind the wallet is no longer
// listed and a fresh Start+Complete attaches it cleanly (proving the row
// was truly removed, not soft-deleted).
func TestSiwsUnbindRemoves(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, sender := newTestService(t, pool)

	_, _ = svc.RequestMagicLink(context.Background(), "carol@example.com", "", "", store.IntentSignIn)
	token := extractTokenFromLink(t, sender.Inbox[0].TextBody)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	bundle, _ := svc.CompleteMagicLink(context.Background(), token, req)
	userID := bundle.User.ID

	priv, addr := freshWallet(t)
	start, _ := svc.StartSiwsBinding(context.Background(), userID, addr)
	sig := signChallenge(priv, start.Challenge)
	if _, err := svc.CompleteSiwsBinding(context.Background(), userID, addr, sig, false, req); err != nil {
		t.Fatal(err)
	}

	if err := svc.UnbindWallet(context.Background(), userID, addr); err != nil {
		t.Fatalf("UnbindWallet: %v", err)
	}

	bindings, _ := svc.ListBoundWallets(context.Background(), userID)
	if len(bindings) != 0 {
		t.Fatalf("expected 0 bindings post-unbind, got %d", len(bindings))
	}

	// A second Unbind must report not-found.
	if err := svc.UnbindWallet(context.Background(), userID, addr); err == nil {
		t.Fatalf("double-unbind should have errored")
	}

	// Re-bind succeeds — challenge invalidation was wallet-scoped.
	start2, _ := svc.StartSiwsBinding(context.Background(), userID, addr)
	sig2 := signChallenge(priv, start2.Challenge)
	if _, err := svc.CompleteSiwsBinding(context.Background(), userID, addr, sig2, false, req); err != nil {
		t.Fatalf("rebind: %v", err)
	}
}

// TestSiwsAutoBindOnFirstAuth: caller has no User → create_if_missing
// mints a fresh one and returns a sign-in bundle.
func TestSiwsAutoBindOnFirstAuth(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, _ := newTestService(t, pool)

	priv, addr := freshWallet(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	start, err := svc.StartSiwsBinding(context.Background(), uuid.Nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	sig := signChallenge(priv, start.Challenge)
	res, err := svc.CompleteSiwsBinding(context.Background(), uuid.Nil, addr, sig, true, req)
	if err != nil {
		t.Fatalf("CompleteSiwsBinding: %v", err)
	}
	if !res.NewUser {
		t.Errorf("NewUser=false")
	}
	if res.Bundle == nil {
		t.Fatalf("Bundle missing on create_if_missing path")
	}
	if res.Bundle.AccessToken == "" {
		t.Errorf("empty access token")
	}
	claims, err := svc.Signer.Verify(res.Bundle.AccessToken)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(claims.SolanaAddresses) != 1 || claims.SolanaAddresses[0] != addr {
		t.Errorf("SolanaAddresses claim: %v", claims.SolanaAddresses)
	}
}

// TestSiwsRejectsWrongSignature: signing with key A but claiming key B
// must reject (defence-in-depth against the trivial spoof).
func TestSiwsRejectsWrongSignature(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, _ := newTestService(t, pool)

	privA, addrA := freshWallet(t)
	_, addrB := freshWallet(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	start, err := svc.StartSiwsBinding(context.Background(), uuid.Nil, addrA)
	if err != nil {
		t.Fatal(err)
	}
	// Sign with A's key but submit B as the address — challenge for B
	// doesn't exist → ErrChallengeNotFound, NOT a signature pass.
	sig := signChallenge(privA, start.Challenge)
	if _, err := svc.CompleteSiwsBinding(context.Background(), uuid.Nil, addrB, sig, true, req); err == nil {
		t.Fatalf("submitting wrong address should fail")
	}
}
