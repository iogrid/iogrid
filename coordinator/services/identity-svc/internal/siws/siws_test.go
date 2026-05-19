package siws

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

// signingKey returns a fresh ed25519 keypair plus the base58 address
// every test reuses to mimic a Phantom-connected wallet.
func signingKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	return priv, EncodeAddress(pub)
}

// TestVerifySignature_Roundtrip exercises the canonical happy path: we
// build the message, sign it, base58-encode the signature, and verify.
func TestVerifySignature_Roundtrip(t *testing.T) {
	priv, addr := signingKey(t)

	nonce, err := NewNonce()
	if err != nil {
		t.Fatalf("NewNonce: %v", err)
	}
	msg := BuildMessage("iogrid.org", addr, nonce)

	sig := ed25519.Sign(priv, []byte(msg))
	if err := VerifySignature(addr, msg, EncodeSignature(sig)); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
}

// TestVerifySignature_WrongMessage proves a signature is bound to the
// exact bytes — even a single-byte change makes the verification fail.
func TestVerifySignature_WrongMessage(t *testing.T) {
	priv, addr := signingKey(t)
	nonce, _ := NewNonce()
	msg := BuildMessage("iogrid.org", addr, nonce)
	sig := ed25519.Sign(priv, []byte(msg))

	tampered := msg + " "
	err := VerifySignature(addr, tampered, EncodeSignature(sig))
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

// TestVerifySignature_WrongKey: signing with key A but claiming key B
// must reject. Defence-in-depth against the trivial spoof.
func TestVerifySignature_WrongKey(t *testing.T) {
	privA, addrA := signingKey(t)
	_, addrB := signingKey(t)

	nonce, _ := NewNonce()
	msg := BuildMessage("iogrid.org", addrA, nonce)
	sig := ed25519.Sign(privA, []byte(msg))

	err := VerifySignature(addrB, msg, EncodeSignature(sig))
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature with mismatched key, got %v", err)
	}
}

// TestDecodeAddress_BadInputs collects every shape we reject up front so
// we never feed garbage to ed25519.Verify.
func TestDecodeAddress_BadInputs(t *testing.T) {
	cases := []struct {
		name string
		addr string
	}{
		{"empty", ""},
		{"not-base58", "!@#$%"},
		// 16 bytes when decoded — wrong length.
		{"wrong-length", "11111111111111111111"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if _, err := DecodeAddress(c.addr); !errors.Is(err, ErrInvalidAddress) {
				t.Fatalf("expected ErrInvalidAddress for %q, got %v", c.addr, err)
			}
		})
	}
}

// TestVerifySignature_BadSignatureBytes: 70-byte signature, malformed
// base58 — both must be rejected before reaching the verifier.
func TestVerifySignature_BadSignatureBytes(t *testing.T) {
	_, addr := signingKey(t)
	msg := BuildMessage("iogrid.org", addr, "deadbeef")

	if err := VerifySignature(addr, msg, "!@#$"); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("not-base58: %v", err)
	}
	// 70-byte signature payload. Base58("...") would still decode, but
	// length check trips.
	short := EncodeSignature(make([]byte, 70))
	if err := VerifySignature(addr, msg, short); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("wrong-length: %v", err)
	}
}

// TestMemoryChallengeStore_PutConsume happy path.
func TestMemoryChallengeStore_PutConsume(t *testing.T) {
	store := NewMemoryChallengeStore()
	rec := ChallengeRecord{
		WalletAddress: "addr1",
		Nonce:         "nonce1",
		UserID:        "user1",
		Message:       "msg",
	}
	if err := store.Put(context.Background(), rec, time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Consume(context.Background(), "addr1")
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if got.Nonce != rec.Nonce || got.Message != rec.Message || got.UserID != rec.UserID {
		t.Fatalf("got %+v want %+v", got, rec)
	}
}

// TestMemoryChallengeStore_ReplayBlocked: a successful Consume must
// invalidate the record so a stolen challenge cannot be re-used.
func TestMemoryChallengeStore_ReplayBlocked(t *testing.T) {
	store := NewMemoryChallengeStore()
	rec := ChallengeRecord{WalletAddress: "addr1", Nonce: "nonce1", Message: "msg"}
	_ = store.Put(context.Background(), rec, time.Minute)

	if _, err := store.Consume(context.Background(), "addr1"); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if _, err := store.Consume(context.Background(), "addr1"); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("replay should have returned ErrChallengeNotFound, got %v", err)
	}
}

// TestMemoryChallengeStore_Expiry: a challenge past its TTL must not
// resolve.
func TestMemoryChallengeStore_Expiry(t *testing.T) {
	store := NewMemoryChallengeStore()
	rec := ChallengeRecord{WalletAddress: "addr1", Nonce: "nonce1", Message: "msg"}
	_ = store.Put(context.Background(), rec, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if _, err := store.Consume(context.Background(), "addr1"); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("expiry: got %v", err)
	}
}

// TestBuildMessage_Format pins the canonical message shape — a regression
// here means existing signed challenges would silently stop verifying.
func TestBuildMessage_Format(t *testing.T) {
	got := BuildMessage("iogrid.org", "Wallet1", "abcd")
	want := "iogrid.org wants you to sign in with your Solana account: Wallet1\n\nNonce: abcd"
	if got != want {
		t.Fatalf("BuildMessage:\n got: %q\nwant: %q", got, want)
	}
}
