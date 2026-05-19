package handlers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newDaemonCSRPEM mints an ECDSA-P256 keypair, builds a PKCS#10 CSR
// with subject CN=daemon-pair-pending (matching what the Rust daemon's
// rcgen path now produces), and returns both the CSR PEM and the
// private key the test can use to round-trip-verify the signed cert.
func newDaemonCSRPEM(t *testing.T) (csrPEM string, priv *ecdsa.PrivateKey) {
	t.Helper()
	var err error
	priv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.CertificateRequest{}
	tmpl.Subject.CommonName = "daemon-pair-pending"
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, priv)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	csrPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: der,
	}))
	return
}

// TestPairDaemonREST_HappyPath exercises the JSON shim end-to-end:
// issue a token via the store, POST the daemon's PairingRequest shape
// with a real CSR, expect the daemon's PairingResponse shape back AND
// the issued cert's public key MUST equal the CSR's public key.
func TestPairDaemonREST_HappyPath(t *testing.T) {
	h := newTestHandler(t)
	tok, _ := h.Store.IssuePairingToken(context.Background(), "owner-rest", 0)
	csrPEM, priv := newDaemonCSRPEM(t)

	body, _ := json.Marshal(map[string]string{
		"pairing_token": tok,
		"csr_pem":       csrPEM,
		"display_name":  "Laptop",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/pair", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.PairDaemonREST(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp pairingRESTResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	if resp.ProviderID == "" {
		t.Fatalf("expected provider_id, got empty")
	}
	if !strings.Contains(resp.CertPEM, "BEGIN CERTIFICATE") {
		t.Fatalf("expected cert PEM, got %q", resp.CertPEM)
	}
	if !strings.Contains(resp.ServerCAPem, "BEGIN CERTIFICATE") {
		t.Fatalf("expected CA PEM, got %q", resp.ServerCAPem)
	}

	// CRITICAL #235 invariant: the issued certificate's public key
	// MUST be exactly the public key the daemon supplied in the CSR.
	// The server is forbidden from generating its own keypair.
	leafBlock, _ := pem.Decode([]byte(resp.CertPEM))
	if leafBlock == nil {
		t.Fatalf("leaf PEM did not decode")
	}
	leaf, err := x509.ParseCertificate(leafBlock.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	leafPub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("leaf pubkey wrong type %T", leaf.PublicKey)
	}
	if leafPub.X.Cmp(priv.PublicKey.X) != 0 || leafPub.Y.Cmp(priv.PublicKey.Y) != 0 {
		t.Fatalf("issued cert's public key does not match CSR's public key")
	}
}

// TestPairDaemonREST_RejectsEmptyCSR — the legacy silent-fallback path
// is gone. Without csr_pem (or the back-compat daemon_public_key_b64)
// providers-svc must return 400 InvalidArgument, never a fabricated
// keypair that the daemon can't verify it owns.
func TestPairDaemonREST_RejectsEmptyCSR(t *testing.T) {
	h := newTestHandler(t)
	tok, _ := h.Store.IssuePairingToken(context.Background(), "owner-rest-empty", 0)
	body, _ := json.Marshal(map[string]string{
		"pairing_token": tok,
		"csr_pem":       "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/pair", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.PairDaemonREST(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "csr_pem required") {
		t.Fatalf("expected csr_pem-required error, got %s", rr.Body.String())
	}
}

// TestPairDaemonREST_RejectsMalformedCSR — bogus PEM, not a CSR.
func TestPairDaemonREST_RejectsMalformedCSR(t *testing.T) {
	h := newTestHandler(t)
	tok, _ := h.Store.IssuePairingToken(context.Background(), "owner-rest-bad", 0)
	body, _ := json.Marshal(map[string]string{
		"pairing_token": tok,
		"csr_pem":       "-----BEGIN CERTIFICATE REQUEST-----\nnotbase64\n-----END CERTIFICATE REQUEST-----\n",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/pair", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.PairDaemonREST(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPairDaemonREST_BadJSON(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/pair", strings.NewReader("not json"))
	rr := httptest.NewRecorder()
	h.PairDaemonREST(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rr.Code)
	}
}

func TestPairDaemonREST_BadToken(t *testing.T) {
	h := newTestHandler(t)
	csrPEM, _ := newDaemonCSRPEM(t)
	body, _ := json.Marshal(map[string]string{
		"pairing_token": "no-such",
		"csr_pem":       csrPEM,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/pair", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.PairDaemonREST(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPairDaemonREST_MissingToken(t *testing.T) {
	h := newTestHandler(t)
	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/pair", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.PairDaemonREST(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rr.Code)
	}
}

func TestPairDaemonREST_WrongMethod(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/pair", nil)
	rr := httptest.NewRecorder()
	h.PairDaemonREST(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d", rr.Code)
	}
}
