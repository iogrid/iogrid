package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

// boot creates an in-memory vpn-svc instance and returns its test
// HTTP server URL. Cleanup is the caller's responsibility (server.Close()).
func boot(t *testing.T) (*httptest.Server, store.Store) {
	t.Helper()
	st := store.NewMemory()
	r := chi.NewRouter()
	logger := slog.Default()
	if err := Mount(r, st, logger); err != nil {
		t.Fatalf("mount: %v", err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, st
}

func postJSON(t *testing.T, url string, body interface{}) (*http.Response, []byte) {
	t.Helper()
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(body)
	resp, err := http.Post(url, "application/json", buf)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody
}

func TestIntegration_SessionLifecycle(t *testing.T) {
	srv, _ := boot(t)

	// 1. Create session
	req := map[string]string{
		"customer_id":  uuid.New().String(),
		"region":       "us-east-1",
		"api_key_hash": "test-hash",
	}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions", req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: status=%d body=%s", resp.StatusCode, body)
	}
	var created map[string]string
	_ = json.Unmarshal(body, &created)
	sessionID := created["session_id"]
	if sessionID == "" {
		t.Fatal("session_id missing in response")
	}
	t.Logf("created session %s", sessionID)

	// 2. Get session
	getResp, getBody := func() (*http.Response, []byte) {
		r, err := http.Get(srv.URL + "/v1/vpn/sessions/" + sessionID)
		if err != nil {
			t.Fatalf("get session: %v", err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r, b
	}()
	if getResp.StatusCode != http.StatusOK {
		t.Errorf("get session: status=%d body=%s", getResp.StatusCode, getBody)
	}

	// 3. Refresh session (heartbeat)
	refreshReq := map[string]interface{}{
		"bytes_in":       12345,
		"bytes_out":      67890,
		"roaming_events": 0,
		"failover_count": 0,
	}
	rresp, rbody := postJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID+"/refresh", refreshReq)
	if rresp.StatusCode != http.StatusOK {
		t.Errorf("refresh: status=%d body=%s", rresp.StatusCode, rbody)
	}

	// 4. Terminate session
	termReq := map[string]string{"reason": "user_initiated"}
	tresp, tbody := postJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID+"/terminate", termReq)
	if tresp.StatusCode != http.StatusOK {
		t.Errorf("terminate: status=%d body=%s", tresp.StatusCode, tbody)
	}
}

func TestIntegration_FailoverNoProviders(t *testing.T) {
	srv, st := boot(t)

	// Create session in a region with no providers
	sessionID := uuid.New()
	_ = st.CreateSession(context.Background(), &store.Session{
		ID:         sessionID,
		CustomerID: uuid.New(),
		Region:     "ap-south-1",
	})

	// Trigger failover — should return 503 because no providers in region
	req := map[string]string{"failure_reason": "endpoint_unreachable"}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID.String()+"/failover", req)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no providers available, got %d body=%s", resp.StatusCode, body)
	}
}

func TestIntegration_FailoverHappyPath(t *testing.T) {
	srv, st := boot(t)
	mem := st.(*store.Memory)

	// Seed 2 providers in eu-west-1 (will round-robin between them)
	primaryProv := uuid.New()
	altProv := uuid.New()
	mem.SeedProvider(primaryProv, "eu-west-1", "healthy")
	mem.SeedProvider(altProv, "eu-west-1", "healthy")

	// Create session pointing at primary
	sessionID := uuid.New()
	_ = mem.CreateSession(context.Background(), &store.Session{
		ID:              sessionID,
		CustomerID:      uuid.New(),
		Region:          "eu-west-1",
		PrimaryProvider: primaryProv,
		CurrentProvider: primaryProv,
	})

	// Trigger failover
	req := map[string]string{"failure_reason": "endpoint_unreachable"}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID.String()+"/failover", req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("failover: status=%d body=%s", resp.StatusCode, body)
	}

	var fr map[string]interface{}
	_ = json.Unmarshal(body, &fr)
	t.Logf("failover response: %s", string(body))
	if fr["status"] != "failover_complete" {
		t.Errorf("status = %v, want 'failover_complete'", fr["status"])
	}
	if fr["new_provider_id"] == fr["old_provider_id"] {
		t.Errorf("new_provider_id (%v) must differ from old_provider_id (%v)",
			fr["new_provider_id"], fr["old_provider_id"])
	}

	// Verify session state was updated
	got, _ := st.GetSession(context.Background(), sessionID)
	if got.FailoverCount != 1 {
		t.Errorf("FailoverCount = %d, want 1", got.FailoverCount)
	}
}
