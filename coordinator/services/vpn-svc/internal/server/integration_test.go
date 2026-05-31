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
	"time"

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

	// Session with a CurrentProvider set so we get past the #535 guard,
	// in a region with no OTHER healthy providers to fail over to.
	sessionID := uuid.New()
	currentProvider := uuid.New()
	_ = st.CreateSession(context.Background(), &store.Session{
		ID:              sessionID,
		CustomerID:      uuid.New(),
		Region:          "ap-south-1",
		PrimaryProvider: currentProvider,
		CurrentProvider: currentProvider,
	})

	// Trigger failover — should return 503 because no alternate providers in region
	req := map[string]string{"failure_reason": "endpoint_unreachable"}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID.String()+"/failover", req)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no alternate providers, got %d body=%s", resp.StatusCode, body)
	}
}

func TestIntegration_WGBindingFlow(t *testing.T) {
	// Closes the loop for #536: customer creates session → daemon polls
	// assigned-sessions → daemon binds provider WG key → customer reads
	// it back via GET /sessions/{id}.
	srv, st := boot(t)
	mem := st.(*store.Memory)

	providerID := uuid.New()
	mem.SeedProvider(providerID, "us-east-1", "healthy")

	// Create + bind session directly so we don't need RequestSession's
	// provider-assignment logic for this slice (that's separate scope).
	sessionID := uuid.New()
	customerID := uuid.New()
	_ = mem.CreateSession(context.Background(), &store.Session{
		ID:              sessionID,
		CustomerID:      customerID,
		Region:          "us-east-1",
		PrimaryProvider: providerID,
		CurrentProvider: providerID,
	})

	// 1. Daemon polls assigned sessions — sees one unbound
	listResp, listBody := func() (*http.Response, []byte) {
		r, err := http.Get(srv.URL + "/v1/vpn/providers/" + providerID.String() + "/assigned-sessions")
		if err != nil {
			t.Fatalf("GET assigned-sessions: %v", err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r, b
	}()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list assigned: status=%d body=%s", listResp.StatusCode, listBody)
	}
	var listOut map[string]interface{}
	_ = json.Unmarshal(listBody, &listOut)
	if count, _ := listOut["count"].(float64); int(count) != 1 {
		t.Errorf("assigned count = %v, want 1", listOut["count"])
	}

	// 2. Daemon binds its WG public key for that session
	bindReq := map[string]string{"provider_wg_public_key": "TESTKEY=daemon-wg-pubkey"}
	bindResp, bindBody := postJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID.String()+"/bind-provider", bindReq)
	if bindResp.StatusCode != http.StatusOK {
		t.Fatalf("bind-provider: status=%d body=%s", bindResp.StatusCode, bindBody)
	}

	// 3. Customer reads session back, sees the bound key
	getResp, getBody := func() (*http.Response, []byte) {
		r, err := http.Get(srv.URL + "/v1/vpn/sessions/" + sessionID.String())
		if err != nil {
			t.Fatalf("GET session: %v", err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r, b
	}()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get session: status=%d body=%s", getResp.StatusCode, getBody)
	}
	var getOut map[string]interface{}
	_ = json.Unmarshal(getBody, &getOut)
	if pubkey, _ := getOut["provider_wg_public_key"].(string); pubkey != "TESTKEY=daemon-wg-pubkey" {
		t.Errorf("provider_wg_public_key after bind = %q, want TESTKEY=daemon-wg-pubkey", pubkey)
	}

	// 4. Daemon polls again — session no longer appears (already bound)
	listResp2, listBody2 := func() (*http.Response, []byte) {
		r, err := http.Get(srv.URL + "/v1/vpn/providers/" + providerID.String() + "/assigned-sessions")
		if err != nil {
			t.Fatalf("re-list GET: %v", err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r, b
	}()
	if listResp2.StatusCode != http.StatusOK {
		t.Fatalf("re-list: status=%d body=%s", listResp2.StatusCode, listBody2)
	}
	var listOut2 map[string]interface{}
	_ = json.Unmarshal(listBody2, &listOut2)
	if count, _ := listOut2["count"].(float64); int(count) != 0 {
		t.Errorf("post-bind assigned count = %v, want 0 (filtered out)", listOut2["count"])
	}
}

func TestIntegration_FailoverNoCurrentProvider(t *testing.T) {
	// Closes #535: session with CurrentProvider=uuid.Nil must return 409,
	// not silently pick the only healthy provider as if it were a failover.
	srv, st := boot(t)
	mem := st.(*store.Memory)
	mem.SeedProvider(uuid.New(), "us-east-1", "healthy")

	sessionID := uuid.New()
	_ = st.CreateSession(context.Background(), &store.Session{
		ID:         sessionID,
		CustomerID: uuid.New(),
		Region:     "us-east-1",
		// CurrentProvider deliberately zero
	})

	req := map[string]string{"failure_reason": "test"}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID.String()+"/failover", req)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 (session has no current provider), got %d body=%s", resp.StatusCode, body)
	}
}

func TestIntegration_RegisterProviderThenHealth(t *testing.T) {
	// The end-to-end lifecycle a daemon goes through: register, then heartbeat.
	srv, st := boot(t)
	providerID := uuid.New()

	// 1. Register (would be the first call from a fresh daemon)
	regReq := map[string]string{"region": "us-east-1"}
	resp, body := postJSON(t, srv.URL+"/v1/vpn/providers/"+providerID.String()+"/register", regReq)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register: status=%d body=%s", resp.StatusCode, body)
	}

	// Verify the provider is now in the store with status=healthy
	providers, _ := st.GetProvidersInRegion(context.Background(), "us-east-1")
	if len(providers) != 1 {
		t.Errorf("expected 1 provider in us-east-1, got %d", len(providers))
	}
	if providers[0].ID != providerID {
		t.Errorf("provider ID = %v, want %v", providers[0].ID, providerID)
	}
	if providers[0].Status != "healthy" {
		t.Errorf("status = %q, want healthy", providers[0].Status)
	}

	// 2. Health probe (would be the periodic heartbeat) — should NOT 404 now
	healthReq := map[string]interface{}{
		"status":      "healthy",
		"at_unix_ms":  time.Now().UnixMilli(),
	}
	hresp, hbody := postJSON(t, srv.URL+"/v1/vpn/providers/"+providerID.String()+"/health", healthReq)
	if hresp.StatusCode != http.StatusOK {
		t.Errorf("health post: status=%d body=%s", hresp.StatusCode, hbody)
	}

	// 3. Re-register (idempotent — should preserve session_count)
	regResp2, _ := postJSON(t, srv.URL+"/v1/vpn/providers/"+providerID.String()+"/register", regReq)
	if regResp2.StatusCode != http.StatusOK {
		t.Errorf("re-register failed: status=%d", regResp2.StatusCode)
	}
}

func TestIntegration_ListProvidersInRegion(t *testing.T) {
	srv, st := boot(t)
	mem := st.(*store.Memory)

	// Seed 3 providers in us-east-1 + 1 in eu-west-1
	mem.SeedProvider(uuid.New(), "us-east-1", "healthy")
	mem.SeedProvider(uuid.New(), "us-east-1", "degraded")
	mem.SeedProvider(uuid.New(), "us-east-1", "offline") // should NOT appear
	mem.SeedProvider(uuid.New(), "eu-west-1", "healthy")

	// us-east-1 should return 2 (offline excluded)
	resp, err := http.Get(srv.URL + "/v1/vpn/regions/us-east-1/providers")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result["region"] != "us-east-1" {
		t.Errorf("region = %v, want us-east-1", result["region"])
	}
	if count, ok := result["count"].(float64); !ok || int(count) != 2 {
		t.Errorf("count = %v, want 2 (offline excluded)", result["count"])
	}

	// eu-west-1 should return 1
	resp2, err := http.Get(srv.URL + "/v1/vpn/regions/eu-west-1/providers")
	if err != nil {
		t.Fatalf("GET eu-west-1: %v", err)
	}
	defer resp2.Body.Close()
	var result2 map[string]interface{}
	_ = json.NewDecoder(resp2.Body).Decode(&result2)
	if count, ok := result2["count"].(float64); !ok || int(count) != 1 {
		t.Errorf("eu-west-1 count = %v, want 1", result2["count"])
	}

	// ap-south-1 (no providers) returns count=0, not error
	resp3, err := http.Get(srv.URL + "/v1/vpn/regions/ap-south-1/providers")
	if err != nil {
		t.Fatalf("GET ap-south-1: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("empty region returned %d, want 200", resp3.StatusCode)
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
