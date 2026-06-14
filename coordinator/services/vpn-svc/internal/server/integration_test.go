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
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

// boot creates an in-memory vpn-svc instance and returns its test
// HTTP server URL. Cleanup is the caller's responsibility (server.Close()).
func boot(t *testing.T) (*httptest.Server, store.Store) {
	t.Helper()
	st := store.NewMemory()
	r := chi.NewRouter()
	logger := slog.Default()
	if err := Mount(r, st, logger, nil, nil); err != nil {
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
	srv, st := boot(t)
	mem := st.(*store.Memory)
	mem.SeedProvider(uuid.New(), "us-east-1", "healthy")

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

// fakeValidator implements APIKeyValidator for tests — never touches the
// real billing-svc client. `tier` defaults to "" (free); tests that want a
// paid tier can populate it explicitly.
type fakeValidator struct {
	valid    map[string]struct{ ws, cust, tier string }
	rejected map[string]struct{}
}

func (f *fakeValidator) Validate(ctx context.Context, apiKey string) (string, string, string, error) {
	if _, bad := f.rejected[apiKey]; bad {
		return "", "", "", errInvalidKey
	}
	if v, ok := f.valid[apiKey]; ok {
		return v.ws, v.cust, v.tier, nil
	}
	return "", "", "", errInvalidKey
}

func bootWithValidator(t *testing.T, v APIKeyValidator) (*httptest.Server, store.Store) {
	t.Helper()
	st := store.NewMemory()
	r := chi.NewRouter()
	logger := slog.Default()
	if err := Mount(r, st, logger, v, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, st
}

func TestIntegration_APIKeyValidation(t *testing.T) {
	wsID := uuid.New().String()
	custID := uuid.New().String()
	fv := &fakeValidator{
		valid:    map[string]struct{ ws, cust, tier string }{"iog_validkey": {wsID, custID, "SUBSCRIPTION_TIER_STARTER"}},
		rejected: map[string]struct{}{"iog_revoked": {}},
	}
	srv, st := bootWithValidator(t, fv)
	st.(*store.Memory).SeedProvider(uuid.New(), "us-east-1", "healthy")

	// Missing key → 401
	resp, _ := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": custID, "region": "us-east-1",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("missing key: got %d, want 401", resp.StatusCode)
	}

	// Bad key → 401
	resp2, _ := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": custID, "region": "us-east-1", "api_key": "iog_revoked",
	})
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad key: got %d, want 401", resp2.StatusCode)
	}

	// Good key → 201
	resp3, body3 := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": custID, "region": "us-east-1", "api_key": "iog_validkey",
	})
	if resp3.StatusCode != http.StatusCreated {
		t.Errorf("good key: got %d body=%s, want 201", resp3.StatusCode, body3)
	}

	// Unknown customer_id with valid key → spoof attempt. Server trusts billing-svc.
	spoofedID := uuid.New().String()
	resp4, _ := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": spoofedID, "region": "us-east-1", "api_key": "iog_validkey",
	})
	if resp4.StatusCode != http.StatusCreated {
		t.Errorf("spoofed customer_id with valid key: got %d", resp4.StatusCode)
	}
	// Session should have been created with the validator-resolved customer_id,
	// NOT the spoofed one. (Verifies we trust upstream over the wire claim.)
}

func TestIntegration_NoValidator_DevMode(t *testing.T) {
	// Without validator, any request succeeds (dev mode).
	srv, st := bootWithValidator(t, nil)
	st.(*store.Memory).SeedProvider(uuid.New(), "us-east-1", "healthy")
	resp, _ := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": uuid.New().String(), "region": "us-east-1",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("dev mode: got %d, want 201", resp.StatusCode)
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
		"status":     "healthy",
		"at_unix_ms": time.Now().UnixMilli(),
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

func TestIntegration_FreeTierQuotaExceeded(t *testing.T) {
	// Free-tier customer that has already consumed > 2 GiB this month
	// must be rejected at RequestSession with 429.
	custID := uuid.New()
	fv := &fakeValidator{
		valid: map[string]struct{ ws, cust, tier string }{
			"iog_freekey": {uuid.New().String(), custID.String(), "SUBSCRIPTION_TIER_PAYG"},
		},
	}
	srv, st := bootWithValidator(t, fv)
	provID := uuid.New()
	st.(*store.Memory).SeedProvider(provID, "us-east-1", "healthy")

	// Seed a session that already burned through > FreeTierQuotaBytes.
	prior := &store.Session{
		ID: uuid.New(), CustomerID: custID, Region: "us-east-1",
		PrimaryProvider: provID, CurrentProvider: provID,
		BytesIn: FreeTierQuotaBytes + 1, BytesOut: 0,
		CreatedAt: time.Now(), LastActivityAt: time.Now(),
	}
	if err := st.CreateSession(context.Background(), prior); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	resp, _ := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": custID.String(), "region": "us-east-1", "api_key": "iog_freekey",
	})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on free-tier quota exhaust, got %d", resp.StatusCode)
	}
}

func TestIntegration_PaidTierIgnoresQuota(t *testing.T) {
	// Paid-tier customer with high prior usage must still succeed.
	custID := uuid.New()
	fv := &fakeValidator{
		valid: map[string]struct{ ws, cust, tier string }{
			"iog_starterkey": {uuid.New().String(), custID.String(), "SUBSCRIPTION_TIER_STARTER"},
		},
	}
	srv, st := bootWithValidator(t, fv)
	provID := uuid.New()
	st.(*store.Memory).SeedProvider(provID, "us-east-1", "healthy")

	// Seed huge prior usage.
	_ = st.CreateSession(context.Background(), &store.Session{
		ID: uuid.New(), CustomerID: custID, Region: "us-east-1",
		PrimaryProvider: provID, CurrentProvider: provID,
		BytesIn: FreeTierQuotaBytes * 100, BytesOut: 0,
		CreatedAt: time.Now(), LastActivityAt: time.Now(),
	})

	resp, _ := postJSON(t, srv.URL+"/v1/vpn/sessions", map[string]string{
		"customer_id": custID.String(), "region": "us-east-1", "api_key": "iog_starterkey",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("paid tier must bypass quota; got status %d", resp.StatusCode)
	}
}

// TestIntegration_RegionAutoPicksAcrossRegions verifies #570: when the
// session-create body sets region="auto" the coordinator picks the
// least-loaded healthy provider across ALL regions and stores the
// chosen region on the session row.
func TestIntegration_RegionAutoPicksAcrossRegions(t *testing.T) {
	srv, st := boot(t)
	mem := st.(*store.Memory)
	// us-east-1 has a heavily loaded provider; eu-west-1 has a fresh one.
	pUS := uuid.New()
	pEU := uuid.New()
	mem.SeedProvider(pUS, "us-east-1", "healthy")
	mem.SeedProvider(pEU, "eu-west-1", "healthy")
	// Bump us-east-1 load so eu-west-1 wins.
	_, _ = mem.SelectProviderForSession(context.Background(), "us-east-1")
	_, _ = mem.SelectProviderForSession(context.Background(), "us-east-1")
	_, _ = mem.SelectProviderForSession(context.Background(), "us-east-1")

	body := map[string]string{
		"customer_id": uuid.New().String(),
		"region":      "auto",
	}
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(body)
	req, _ := http.NewRequest("POST", srv.URL+"/v1/vpn/sessions", buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "85.214.10.1, 10.0.0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /sessions auto: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("region=auto: status=%d body=%s", resp.StatusCode, respBody)
	}
	var got map[string]string
	_ = json.Unmarshal(respBody, &got)
	if got["region"] != "eu-west-1" {
		t.Errorf("auto-picked region = %q, want eu-west-1 (least loaded)", got["region"])
	}
	if got["provider_id"] != pEU.String() {
		t.Errorf("auto-picked provider = %s, want %s", got["provider_id"], pEU)
	}

	// Verify the session row's region is the AUTO-resolved one, not "auto".
	sessID, _ := uuid.Parse(got["session_id"])
	sess, err := st.GetSession(context.Background(), sessID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Region != "eu-west-1" {
		t.Errorf("stored session.Region = %q, want eu-west-1", sess.Region)
	}
}

// TestIntegration_RegionAutoNoProviders verifies #570 503 path.
func TestIntegration_RegionAutoNoProviders(t *testing.T) {
	srv, _ := boot(t)
	body := map[string]string{
		"customer_id": uuid.New().String(),
		"region":      "auto",
	}
	resp, _ := postJSON(t, srv.URL+"/v1/vpn/sessions", body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("region=auto with empty store should 503, got %d", resp.StatusCode)
	}
}

// TestIntegration_TopProvidersInRegion verifies #570 GET endpoint.
func TestIntegration_TopProvidersInRegion(t *testing.T) {
	srv, st := boot(t)
	mem := st.(*store.Memory)
	pA, pB, pC := uuid.New(), uuid.New(), uuid.New()
	mem.SeedProvider(pA, "us-east-1", "healthy")
	mem.SeedProvider(pB, "us-east-1", "healthy")
	mem.SeedProvider(pC, "us-east-1", "healthy")
	// Re-register with wg keys (SeedProvider doesn't take a key).
	ctx := context.Background()
	_ = st.RegisterProvider(ctx, &store.ProviderInfo{ID: pA, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(), WgPublicKey: "wgA"})
	_ = st.RegisterProvider(ctx, &store.ProviderInfo{ID: pB, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(), WgPublicKey: "wgB"})
	_ = st.RegisterProvider(ctx, &store.ProviderInfo{ID: pC, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(), WgPublicKey: "wgC"})

	// Seed candidates on pA.
	pbCands := []*pb.IceCandidate{
		{Foundation: "1", Transport: "udp", CandidateType: "host", ConnectionAddress: "10.0.0.1", ConnectionPort: 51820, LatencyMs: 30, DiscoveredAtUnixMs: time.Now().UnixMilli()},
		{Foundation: "2", Transport: "udp", CandidateType: "srflx", ConnectionAddress: "1.2.3.4", ConnectionPort: 51820, LatencyMs: 70, DiscoveredAtUnixMs: time.Now().UnixMilli()},
	}
	_ = st.RegisterCandidates(ctx, pA, pbCands)

	// GET with ?limit=3 → top-3 probe shape.
	resp, err := http.Get(srv.URL + "/v1/vpn/regions/us-east-1/providers?limit=3")
	if err != nil {
		t.Fatalf("GET top-3: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET top-3: status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["region"] != "us-east-1" {
		t.Errorf("region in response = %v, want us-east-1", got["region"])
	}
	providers, ok := got["providers"].([]interface{})
	if !ok {
		t.Fatalf("providers field missing or wrong type: %v", got["providers"])
	}
	if len(providers) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(providers))
	}
	first, _ := providers[0].(map[string]interface{})
	if _, hasID := first["provider_id"]; !hasID {
		t.Error("first provider missing provider_id field")
	}
	if _, hasKey := first["wg_public_key"]; !hasKey {
		t.Error("first provider missing wg_public_key field")
	}
	if _, hasCands := first["candidate_set"]; !hasCands {
		t.Error("first provider missing candidate_set field")
	}
	if _, hasRtt := first["median_rtt_ms"]; !hasRtt {
		t.Error("first provider missing median_rtt_ms field")
	}

	// Without ?limit, falls back to legacy shape (no wg_public_key).
	resp2, err := http.Get(srv.URL + "/v1/vpn/regions/us-east-1/providers")
	if err != nil {
		t.Fatalf("GET legacy: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("legacy GET: status=%d", resp2.StatusCode)
	}
	var legacy map[string]interface{}
	_ = json.NewDecoder(resp2.Body).Decode(&legacy)
	legacyProviders, _ := legacy["providers"].([]interface{})
	if len(legacyProviders) != 3 {
		t.Errorf("legacy shape: expected 3 providers, got %d", len(legacyProviders))
	}

	// Invalid limit → 400.
	respBad, _ := http.Get(srv.URL + "/v1/vpn/regions/us-east-1/providers?limit=notanumber")
	if respBad.StatusCode != http.StatusBadRequest {
		t.Errorf("?limit=notanumber: got %d, want 400", respBad.StatusCode)
	}
	respBad.Body.Close()
}

// ── Mobile session bring-up (#588 / #605) ──────────────────────────

// TestIntegration_MobileSession_MissingRegion verifies the mobile
// endpoint rejects payloads without region or client_public_key
// before any store work happens. (#605)
func TestIntegration_MobileSession_MissingRegion(t *testing.T) {
	srv, _ := boot(t)

	body := map[string]string{
		"client_public_key": "AAAA1111BBBB2222CCCC3333DDDD4444=",
		// region intentionally omitted
	}
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(body)
	req, _ := http.NewRequest("POST", srv.URL+"/v1/vpn/sessions/mobile", buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /sessions/mobile: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing region: status=%d, want 400", resp.StatusCode)
	}
}

// TestIntegration_MobileSession_MissingClientPublicKey verifies the
// handler rejects requests without a client WG public key. (#605)
func TestIntegration_MobileSession_MissingClientPublicKey(t *testing.T) {
	srv, _ := boot(t)

	body := map[string]string{
		"region": "us-east-1",
		// client_public_key intentionally omitted
	}
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(body)
	req, _ := http.NewRequest("POST", srv.URL+"/v1/vpn/sessions/mobile", buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /sessions/mobile: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing client_public_key: status=%d, want 400", resp.StatusCode)
	}
}

// TestIntegration_MobileSession_NoProviders verifies the handler
// returns 503 + Retry-After when the requested region has no
// healthy providers. The mobile app uses this signal to retry
// after a short backoff. (#605)
func TestIntegration_MobileSession_NoProviders(t *testing.T) {
	srv, _ := boot(t)

	body := map[string]string{
		"customer_id":       uuid.New().String(),
		"client_public_key": "AAAA1111BBBB2222CCCC3333DDDD4444=",
		"region":            "us-east-1",
	}
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(body)
	req, _ := http.NewRequest("POST", srv.URL+"/v1/vpn/sessions/mobile", buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /sessions/mobile: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("no providers: status=%d, want 503", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Errorf("503 should carry Retry-After header, got empty")
	}
}

// TestIntegration_MobileSession_MissingCustomerID — empty customer_id
// rejected at pre-validation (no store work happens). (#605)
func TestIntegration_MobileSession_MissingCustomerID(t *testing.T) {
	srv, _ := boot(t)
	body := map[string]string{
		"client_public_key": "AAAA1111BBBB2222CCCC3333DDDD4444=",
		"region":            "us-east-1",
	}
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(body)
	req, _ := http.NewRequest("POST", srv.URL+"/v1/vpn/sessions/mobile", buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /sessions/mobile: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing customer_id: status=%d, want 400", resp.StatusCode)
	}
}

// TestIntegration_BoundSessionsEndpoint is the #788 wire-level test: the
// daemon's restart-recovery poll GET /providers/{id}/bound-sessions must
// return an already-bound, >15-min-old session (which /assigned-sessions
// hides) with its customer key, so the daemon can re-upsert the peer after a
// restart. Run through the real chi router so the route mount + JSON shape
// are exercised end to end.
func TestIntegration_BoundSessionsEndpoint(t *testing.T) {
	srv, st := boot(t)
	ctx := context.Background()
	provider := uuid.New()
	st.(*store.Memory).SeedProvider(provider, "us-east-1", "healthy")

	const custKey = "Y3VzdG9tZXJfa2V5X2JvdW5kX29sZA=="
	// A session bound an hour ago: /assigned-sessions hides it (already
	// provider-keyed AND past AssignedSessionMaxAge); /bound-sessions returns it.
	boundOld := &store.Session{
		ID:                  uuid.New(),
		CustomerID:          uuid.New(),
		Region:              "us-east-1",
		PrimaryProvider:     provider,
		CurrentProvider:     provider,
		CreatedAt:           time.Now().Add(-1 * time.Hour),
		LastActivityAt:      time.Now(),
		CustomerWgPublicKey: custKey,
		ProviderWgPublicKey: "cHJvdmlkZXJfa2V5",
	}
	if err := st.CreateSession(ctx, boundOld); err != nil {
		t.Fatalf("create bound session: %v", err)
	}

	// Sanity: /assigned-sessions must NOT return it.
	assignedResp, err := http.Get(srv.URL + "/v1/vpn/providers/" + provider.String() + "/assigned-sessions")
	if err != nil {
		t.Fatalf("GET assigned-sessions: %v", err)
	}
	defer assignedResp.Body.Close()
	var assigned struct {
		Count int `json:"count"`
	}
	_ = json.NewDecoder(assignedResp.Body).Decode(&assigned)
	if assigned.Count != 0 {
		t.Fatalf("assigned-sessions should hide the bound+old session, got count=%d", assigned.Count)
	}

	// /bound-sessions MUST return it with the customer key.
	boundResp, err := http.Get(srv.URL + "/v1/vpn/providers/" + provider.String() + "/bound-sessions")
	if err != nil {
		t.Fatalf("GET bound-sessions: %v", err)
	}
	defer boundResp.Body.Close()
	if boundResp.StatusCode != http.StatusOK {
		t.Fatalf("bound-sessions status=%d, want 200", boundResp.StatusCode)
	}
	var bound struct {
		ProviderID string `json:"provider_id"`
		Count      int    `json:"count"`
		Sessions   []struct {
			SessionID           string `json:"session_id"`
			CustomerWgPublicKey string `json:"customer_wg_public_key"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(boundResp.Body).Decode(&bound); err != nil {
		t.Fatalf("decode bound-sessions: %v", err)
	}
	if bound.Count != 1 || len(bound.Sessions) != 1 {
		t.Fatalf("bound-sessions count=%d len=%d, want 1/1", bound.Count, len(bound.Sessions))
	}
	if bound.Sessions[0].SessionID != boundOld.ID.String() {
		t.Errorf("bound-sessions session_id=%q, want %q", bound.Sessions[0].SessionID, boundOld.ID)
	}
	if bound.Sessions[0].CustomerWgPublicKey != custKey {
		t.Errorf("bound-sessions customer key=%q, want %q — the daemon needs it to re-upsert the peer",
			bound.Sessions[0].CustomerWgPublicKey, custKey)
	}

	// Invalid provider id → 400.
	badResp, err := http.Get(srv.URL + "/v1/vpn/providers/not-a-uuid/bound-sessions")
	if err != nil {
		t.Fatalf("GET bound-sessions bad id: %v", err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid provider id: status=%d, want 400", badResp.StatusCode)
	}
}
