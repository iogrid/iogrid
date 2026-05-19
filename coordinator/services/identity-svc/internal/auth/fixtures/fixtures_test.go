// Package fixtures hosts the committed dev JWT keypair. The companion
// test verifies the PEM files round-trip through tokens.NewSigner so
// fixture rot can't silently break local-dev or e2e boot.
package fixtures

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

func TestFixtureKeypair_RoundTripsThroughSigner(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)

	signer, err := tokens.NewSigner(tokens.SignerConfig{
		PrivateKeyPath: filepath.Join(dir, "jwt_test.key"),
		PublicKeyPath:  filepath.Join(dir, "jwt_test.pub"),
		KeyID:          "dev-fixture",
		Issuer:         "https://test.iogrid.org",
		Audience:       []string{"gateway-bff"},
		AccessTokenTTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewSigner against committed fixture: %v", err)
	}
	tok, _, err := signer.IssueAccessToken(uuid.New(), uuid.New(), "dev@iogrid.org", nil, nil, false, nil)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if _, err := signer.Verify(tok); err != nil {
		t.Fatalf("Verify against committed fixture: %v", err)
	}
}
