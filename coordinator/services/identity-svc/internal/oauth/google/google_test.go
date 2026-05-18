package google

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMemoryStateStore_RoundTrip(t *testing.T) {
	store := newMemoryStateStore()
	want := pendingState{CodeVerifier: "abc", ReturnTo: "/x", Nonce: "n"}
	if err := store.put(context.Background(), "key1", want, time.Minute); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := store.pop(context.Background(), "key1")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if got.CodeVerifier != want.CodeVerifier || got.ReturnTo != want.ReturnTo || got.Nonce != want.Nonce {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// Second pop must fail — single-use.
	if _, err := store.pop(context.Background(), "key1"); err == nil {
		t.Fatalf("second pop should fail")
	}
}

func TestMemoryStateStore_ExpiredEntryRejected(t *testing.T) {
	store := newMemoryStateStore()
	store.put(context.Background(), "k", pendingState{}, time.Nanosecond)
	time.Sleep(2 * time.Millisecond)
	if _, err := store.pop(context.Background(), "k"); err == nil {
		t.Fatalf("expired entry should not pop")
	}
}

func TestS256Challenge_RFCVector(t *testing.T) {
	// RFC 7636 §A.3 reference.
	got := s256Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got != want {
		t.Fatalf("s256Challenge mismatch: got %q want %q", got, want)
	}
}

func TestRandString_NonEmptyAndDistinct(t *testing.T) {
	a, err := randString(16)
	if err != nil {
		t.Fatalf("randString: %v", err)
	}
	if a == "" {
		t.Fatalf("randString returned empty")
	}
	if strings.ContainsAny(a, "+/=") {
		t.Fatalf("randString returned non-URL-safe chars: %q", a)
	}
	b, _ := randString(16)
	if a == b {
		t.Fatalf("randString returned same value twice")
	}
}

func TestDefaultScopes_IncludesOpenID(t *testing.T) {
	scopes := DefaultScopes()
	if len(scopes) < 3 {
		t.Fatalf("expected >=3 scopes, got %v", scopes)
	}
	var hasOpenID, hasEmail, hasUserEmails bool
	for _, s := range scopes {
		switch {
		case s == "openid":
			hasOpenID = true
		case s == "email":
			hasEmail = true
		case strings.Contains(s, "user.emails.read"):
			hasUserEmails = true
		}
	}
	if !hasOpenID || !hasEmail || !hasUserEmails {
		t.Fatalf("missing required scopes: %v", scopes)
	}
}
