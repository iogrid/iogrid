package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPairDaemonREST_HappyPath exercises the JSON shim end-to-end:
// issue a token via the store, POST the daemon's PairingRequest shape,
// expect the daemon's PairingResponse shape back.
func TestPairDaemonREST_HappyPath(t *testing.T) {
	h := newTestHandler(t)
	tok, _ := h.Store.IssuePairingToken(context.Background(), "owner-rest", 0)

	body, _ := json.Marshal(map[string]string{
		"pairing_token": tok,
		"csr_pem":       "",
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
	body, _ := json.Marshal(map[string]string{"pairing_token": "no-such"})
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
