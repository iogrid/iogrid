package tokens

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// freshSigner mints an in-memory RSA key + Signer for tests.
func freshSigner(t *testing.T) *Signer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen rsa: %v", err)
	}
	return NewSignerFromKeys(priv, "test", "https://test.iogrid.org", []string{"gateway-bff"}, 15*time.Minute)
}

func TestRandom32_ReturnsURLSafeBase64(t *testing.T) {
	a, err := Random32()
	if err != nil {
		t.Fatalf("Random32: %v", err)
	}
	if len(a) < 32 {
		t.Fatalf("Random32 too short: %d", len(a))
	}
	if strings.ContainsAny(a, "+/=") {
		t.Fatalf("Random32 returned non-URL-safe chars: %q", a)
	}
	b, _ := Random32()
	if a == b {
		t.Fatalf("Random32 returned same value twice (entropy bug?)")
	}
}

func TestSHA256Hex_IsStable(t *testing.T) {
	got := SHA256Hex("hello")
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("SHA256Hex(hello) = %q, want %q", got, want)
	}
}

func TestPKCEChallengeS256_MatchesRFC7636Vectors(t *testing.T) {
	// Vector from RFC 7636 §A.3.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	got := PKCEChallengeS256(verifier)
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got != want {
		t.Fatalf("PKCEChallengeS256 = %q, want %q", got, want)
	}
}

func TestSigner_RoundTrip(t *testing.T) {
	signer := freshSigner(t)
	userID := uuid.New()
	sessionID := uuid.New()
	tok, exp, err := signer.IssueAccessToken(userID, sessionID, "alice@example.com",
		[]string{"USER_ROLE_PROVIDER"}, []string{"google"}, false)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if exp.Before(time.Now()) {
		t.Fatalf("expiry %v is in the past", exp)
	}
	claims, err := signer.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != userID.String() {
		t.Errorf("Subject = %q, want %q", claims.Subject, userID.String())
	}
	if claims.PrimaryEmail != "alice@example.com" {
		t.Errorf("PrimaryEmail = %q", claims.PrimaryEmail)
	}
	if claims.StepUp {
		t.Errorf("StepUp = true, want false")
	}
}

func TestSigner_VerifyRejectsTampered(t *testing.T) {
	signer := freshSigner(t)
	tok, _, _ := signer.IssueAccessToken(uuid.New(), uuid.New(), "x@x.x", nil, nil, false)
	// Flip a byte in the payload section.
	bad := tok[:len(tok)-5] + "AAAAA"
	if _, err := signer.Verify(bad); err == nil {
		t.Fatalf("Verify accepted tampered token")
	}
}

func TestSigner_PublicKeyPEM_RoundTrip(t *testing.T) {
	signer := freshSigner(t)
	pem, err := signer.PublicKeyPEM()
	if err != nil {
		t.Fatalf("PublicKeyPEM: %v", err)
	}
	if !strings.Contains(string(pem), "BEGIN PUBLIC KEY") {
		t.Fatalf("PEM missing BEGIN header: %s", pem)
	}
}
