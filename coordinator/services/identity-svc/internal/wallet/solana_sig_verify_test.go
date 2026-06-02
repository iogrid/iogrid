package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/siws"
)

// helper: mint a fresh ed25519 keypair + base58-encoded address.
func freshKeypair(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 GenerateKey: %v", err)
	}
	return priv, siws.EncodeAddress(pub)
}

func sign(priv ed25519.PrivateKey, msg string) string {
	return siws.EncodeSignature(ed25519.Sign(priv, []byte(msg)))
}

func TestBuildChallenge_RoundTrip(t *testing.T) {
	now := time.Unix(1_750_000_000, 0)
	c := BuildChallenge("abc123", now)
	if c != "iogrid:bind:abc123:1750000000" {
		t.Fatalf("unexpected challenge: %q", c)
	}
	nonce, ts, err := parseChallenge(c)
	if err != nil {
		t.Fatalf("parseChallenge: %v", err)
	}
	if nonce != "abc123" {
		t.Errorf("nonce mismatch: %q", nonce)
	}
	if !ts.Equal(now) {
		t.Errorf("ts mismatch: %v vs %v", ts, now)
	}
}

func TestVerifyBindSignature_Happy(t *testing.T) {
	priv, addr := freshKeypair(t)
	now := time.Now()
	challenge := BuildChallenge("happy-nonce", now)
	sig := sign(priv, challenge)
	if err := VerifyBindSignature(addr, challenge, sig, now); err != nil {
		t.Fatalf("happy path failed: %v", err)
	}
}

func TestVerifyBindSignature_WrongSignature(t *testing.T) {
	priv, addr := freshKeypair(t)
	now := time.Now()
	challenge := BuildChallenge("nonce", now)
	// Sign a DIFFERENT message than the one verified — flips the bytes.
	bogusSig := sign(priv, BuildChallenge("nonce", now.Add(time.Second)))
	err := VerifyBindSignature(addr, challenge, bogusSig, now)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestVerifyBindSignature_WrongPubkey(t *testing.T) {
	priv, _ := freshKeypair(t)
	_, otherAddr := freshKeypair(t)
	now := time.Now()
	challenge := BuildChallenge("nonce", now)
	sig := sign(priv, challenge)
	err := VerifyBindSignature(otherAddr, challenge, sig, now)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature for wrong pubkey, got %v", err)
	}
}

func TestVerifyBindSignature_ExpiredChallenge(t *testing.T) {
	priv, addr := freshKeypair(t)
	signedAt := time.Now().Add(-10 * time.Minute) // older than ChallengeMaxAge
	challenge := BuildChallenge("nonce", signedAt)
	sig := sign(priv, challenge)
	err := VerifyBindSignature(addr, challenge, sig, time.Now())
	if !errors.Is(err, ErrExpiredChallenge) {
		t.Fatalf("expected ErrExpiredChallenge, got %v", err)
	}
}

func TestVerifyBindSignature_FutureSkewBeyondTolerance(t *testing.T) {
	priv, addr := freshKeypair(t)
	signedAt := time.Now().Add(10 * time.Minute) // future-dated past tolerance
	challenge := BuildChallenge("nonce", signedAt)
	sig := sign(priv, challenge)
	err := VerifyBindSignature(addr, challenge, sig, time.Now())
	if !errors.Is(err, ErrExpiredChallenge) {
		t.Fatalf("expected ErrExpiredChallenge for far-future ts, got %v", err)
	}
}

func TestVerifyBindSignature_MalformedChallenge(t *testing.T) {
	priv, addr := freshKeypair(t)
	for _, payload := range []string{
		"",
		"not-iogrid:bind:abc:123",
		"iogrid:bind:abc",
		"iogrid:bind::123",
		"iogrid:bind:abc:notanumber",
	} {
		sig := sign(priv, payload) // signature is valid for payload, but payload format is wrong
		err := VerifyBindSignature(addr, payload, sig, time.Now())
		if !errors.Is(err, ErrMalformedChallenge) {
			t.Errorf("payload %q: expected ErrMalformedChallenge, got %v", payload, err)
		}
	}
}

func TestVerifyBindSignature_InvalidAddress(t *testing.T) {
	now := time.Now()
	challenge := BuildChallenge("nonce", now)
	// Not base58, definitely not 32 bytes.
	err := VerifyBindSignature("not-a-real-base58-address!!!", challenge, "sig", now)
	if !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("expected ErrInvalidAddress, got %v", err)
	}
	// Empty address.
	err = VerifyBindSignature("", challenge, "sig", now)
	if !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("expected ErrInvalidAddress for empty addr, got %v", err)
	}
	// Trim handling.
	err = VerifyBindSignature(strings.Repeat(" ", 5), challenge, "sig", now)
	if !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("expected ErrInvalidAddress for whitespace addr, got %v", err)
	}
}
