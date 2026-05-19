package tokens

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
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

func TestEnsureAutogenKeypair_WritesUsableKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jwt-keys")

	privPath, pubPath, err := EnsureAutogenKeypair(dir)
	if err != nil {
		t.Fatalf("EnsureAutogenKeypair: %v", err)
	}
	if privPath == "" || pubPath == "" {
		t.Fatalf("EnsureAutogenKeypair returned empty paths: %q, %q", privPath, pubPath)
	}

	// Both files exist, PEM-shaped, correct mode.
	privBytes, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatalf("read priv: %v", err)
	}
	if !strings.Contains(string(privBytes), "PRIVATE KEY") {
		t.Errorf("priv PEM missing header: %q", privBytes[:60])
	}
	pubBytes, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("read pub: %v", err)
	}
	if !strings.Contains(string(pubBytes), "PUBLIC KEY") {
		t.Errorf("pub PEM missing header: %q", pubBytes[:60])
	}

	// NewSigner against the autogen paths round-trips a token.
	signer, err := NewSigner(SignerConfig{
		PrivateKeyPath: privPath,
		PublicKeyPath:  pubPath,
		KeyID:          "autogen",
		Issuer:         "https://test.iogrid.org",
		Audience:       []string{"gateway-bff"},
		AccessTokenTTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewSigner against autogen keypair: %v", err)
	}
	tok, _, err := signer.IssueAccessToken(uuid.New(), uuid.New(), "x@x.x", nil, nil, false)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if _, err := signer.Verify(tok); err != nil {
		t.Fatalf("Verify against autogen keypair: %v", err)
	}
}

func TestEnsureAutogenKeypair_RegeneratesOnEachCall(t *testing.T) {
	// Each call writes a fresh keypair — verifies we don't accidentally
	// hand back stale keys when the autogen dir already has files.
	dir := t.TempDir()
	priv1, _, err := EnsureAutogenKeypair(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	bytes1, _ := os.ReadFile(priv1)

	priv2, _, err := EnsureAutogenKeypair(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	bytes2, _ := os.ReadFile(priv2)

	if string(bytes1) == string(bytes2) {
		t.Errorf("EnsureAutogenKeypair returned identical private key on second call — autogen must be ephemeral")
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
