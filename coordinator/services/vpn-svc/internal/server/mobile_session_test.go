package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// --- test plumbing ---------------------------------------------------------
//
// MobileSessionRequests is a package-global CounterVec registered on the
// default Prometheus registry via promauto, so we cannot swap in a fresh
// registry per test without refactoring production code. Instead we read
// the per-label counter value before and after each request and assert the
// DELTA. This is functionally equivalent to a fresh-registry assertion (it
// isolates each test's contribution to a given outcome label) and is the
// canonical pattern for asserting on promauto globals.

// outcomeCount returns the current value of
// iogrid_vpn_svc_mobile_session_requests_total{outcome=<outcome>}.
func outcomeCount(t *testing.T, outcome string) float64 {
	t.Helper()
	return testutil.ToFloat64(MobileSessionRequests.WithLabelValues(outcome))
}

// failingStore embeds a real memory store but forces AllocateInnerIP to
// fail, exercising the handler's internal_error (500) path without needing
// a live Postgres or a contrived schema-drift condition.
type failingStore struct {
	*store.Memory
	allocErr error
}

func (f *failingStore) AllocateInnerIP(ctx context.Context, providerID, sessionID uuid.UUID) (string, error) {
	if f.allocErr != nil {
		return "", f.allocErr
	}
	return f.Memory.AllocateInnerIP(ctx, providerID, sessionID)
}

// mountMobile builds a vpn-svc test server around the supplied store +
// optional validator. Returns the server URL.
func mountMobile(t *testing.T, st store.Store, v APIKeyValidator) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Mount(r, st, logger, v, nil); err != nil {
		t.Fatalf("mount: %v", err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// seedReadyProvider seeds a healthy provider in `region` with a WG public
// key + a fresh srflx ICE candidate so RequestMobileSession.lookupProvider
// resolves a non-empty endpoint (required for the `created` path).
func seedReadyProvider(t *testing.T, st store.Store, region string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	providerID := uuid.New()
	if err := st.RegisterProvider(ctx, &store.ProviderInfo{
		ID:          providerID,
		Region:      region,
		Status:      "healthy",
		LastSeenAt:  time.Now(),
		WgPublicKey: "PROVIDERWGKEY=",
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	cands := []*pb.IceCandidate{
		{
			Foundation:         "1",
			Transport:          "udp",
			CandidateType:      "srflx",
			ConnectionAddress:  "203.0.113.7",
			ConnectionPort:     51820,
			LatencyMs:          40,
			DiscoveredAtUnixMs: time.Now().UnixMilli(),
		},
	}
	if err := st.RegisterCandidates(ctx, providerID, cands); err != nil {
		t.Fatalf("register candidates: %v", err)
	}
	return providerID
}

// postMobile fires a POST /v1/vpn/sessions/mobile with the given body and
// returns the response + read body.
func postMobile(t *testing.T, url string, body map[string]interface{}, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(body)
	req, _ := http.NewRequest("POST", url+"/v1/vpn/sessions/mobile", buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /sessions/mobile: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

// TestRequestMobileSession_OutcomeMetrics is the table-driven coverage for
// RequestMobileSession.Handle: every response path is exercised, the HTTP
// status is asserted, AND the MobileSessionRequests{outcome=...} counter is
// asserted to increment by exactly one on the expected label (#605).
func TestRequestMobileSession_OutcomeMetrics(t *testing.T) {
	validKey := "iog_mobile_valid"
	revokedKey := "iog_mobile_revoked"

	tests := []struct {
		name           string
		validator      APIKeyValidator
		failAlloc      bool
		seedProvider   bool
		body           map[string]interface{}
		wantStatus     int
		wantOutcome    string
		wantRetryAfter bool
	}{
		{
			name:         "created",
			seedProvider: true,
			body: map[string]interface{}{
				"customer_id":       uuid.New().String(),
				"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
				"region":            "us-east-1",
			},
			wantStatus:  http.StatusCreated,
			wantOutcome: "created",
		},
		{
			name:         "no_peer_503",
			seedProvider: false, // no providers anywhere → picker ErrNoPeer
			body: map[string]interface{}{
				"customer_id":       uuid.New().String(),
				"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
				"region":            "us-east-1",
			},
			wantStatus:     http.StatusServiceUnavailable,
			wantOutcome:    "no_peer",
			wantRetryAfter: true,
		},
		{
			name: "bad_request_400_missing_client_public_key",
			body: map[string]interface{}{
				"customer_id": uuid.New().String(),
				"region":      "us-east-1",
				// client_public_key omitted
			},
			wantStatus:  http.StatusBadRequest,
			wantOutcome: "bad_request",
		},
		{
			name: "bad_request_400_bad_uuid",
			body: map[string]interface{}{
				"customer_id":       "not-a-uuid",
				"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
				"region":            "us-east-1",
			},
			wantStatus:  http.StatusBadRequest,
			wantOutcome: "bad_request",
		},
		{
			name: "unauthorized_401_missing_key",
			validator: &fakeValidator{
				valid: map[string]struct{ ws, cust, tier string }{
					validKey: {uuid.New().String(), uuid.New().String(), "SUBSCRIPTION_TIER_STARTER"},
				},
			},
			body: map[string]interface{}{
				"customer_id":       uuid.New().String(),
				"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
				"region":            "us-east-1",
				// api_key omitted → 401
			},
			wantStatus:  http.StatusUnauthorized,
			wantOutcome: "unauthorized",
		},
		{
			name: "unauthorized_401_revoked_key",
			validator: &fakeValidator{
				valid:    map[string]struct{ ws, cust, tier string }{validKey: {uuid.New().String(), uuid.New().String(), "SUBSCRIPTION_TIER_STARTER"}},
				rejected: map[string]struct{}{revokedKey: {}},
			},
			body: map[string]interface{}{
				"customer_id":       uuid.New().String(),
				"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
				"region":            "us-east-1",
				"api_key":           revokedKey,
			},
			wantStatus:  http.StatusUnauthorized,
			wantOutcome: "unauthorized",
		},
		{
			name:         "internal_error_500_alloc_fails",
			seedProvider: true,
			failAlloc:    true,
			body: map[string]interface{}{
				"customer_id":       uuid.New().String(),
				"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
				"region":            "us-east-1",
			},
			wantStatus:  http.StatusInternalServerError,
			wantOutcome: "internal_error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mem := store.NewMemory().(*store.Memory)
			var st store.Store = mem
			if tc.failAlloc {
				st = &failingStore{Memory: mem, allocErr: errors.New("inner-ip alloc deadlock")}
			}
			if tc.seedProvider {
				seedReadyProvider(t, st, "us-east-1")
			}
			srv := mountMobile(t, st, tc.validator)

			before := outcomeCount(t, tc.wantOutcome)
			resp, body := postMobile(t, srv.URL, tc.body, nil)
			after := outcomeCount(t, tc.wantOutcome)

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status=%d, want %d (body=%s)", resp.StatusCode, tc.wantStatus, body)
			}
			if got := after - before; got != 1 {
				t.Errorf("MobileSessionRequests{outcome=%q} delta=%v, want 1", tc.wantOutcome, got)
			}
			if tc.wantRetryAfter && resp.Header.Get("Retry-After") == "" {
				t.Errorf("expected Retry-After header on 503, got none")
			}
		})
	}
}

// TestRequestMobileSession_BindableByDaemon pins #698: a mobile session must
// be visible to the daemon's binder (in /assigned-sessions) WITH the customer
// WG key + WITHOUT a pre-set provider key, so the provider actually upserts
// the customer peer. Before the fix the mobile flow set only ClientPublicKey
// and pre-set provider_wg_public_key, so the session was excluded from the
// binder and the mobile WG handshake silently failed.
func TestRequestMobileSession_BindableByDaemon(t *testing.T) {
	mem := store.NewMemory().(*store.Memory)
	providerID := seedReadyProvider(t, mem, "us-east-1")
	srv := mountMobile(t, mem, nil)

	const clientKey = "CLIENTwgKEY1111AAAA2222BBBB3333CCCC4444DDD="
	resp, body := postMobile(t, srv.URL, map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"client_public_key": clientKey,
		"region":            "us-east-1",
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d body=%s, want 201", resp.StatusCode, body)
	}

	assigned, err := mem.ListAssignedSessions(context.Background(), providerID)
	if err != nil {
		t.Fatalf("ListAssignedSessions: %v", err)
	}
	if len(assigned) != 1 {
		t.Fatalf("assigned-sessions len=%d, want 1 — mobile session must be bindable by the daemon (#698)", len(assigned))
	}
	if assigned[0].CustomerWgPublicKey != clientKey {
		t.Errorf("assigned CustomerWgPublicKey=%q, want %q (binder needs it to upsert the peer)",
			assigned[0].CustomerWgPublicKey, clientKey)
	}
	if assigned[0].ProviderWgPublicKey != "" {
		t.Errorf("mobile session pre-set ProviderWgPublicKey=%q — that re-excludes it from the binder (#698)",
			assigned[0].ProviderWgPublicKey)
	}
}

// TestRequestMobileSession_CreatedResponseShape asserts the success path
// returns the complete WG peer config the PacketTunnelProvider needs in a
// single round-trip (#588 / #605), so the `created` counter increment is
// backed by a genuinely usable response.
func TestRequestMobileSession_CreatedResponseShape(t *testing.T) {
	mem := store.NewMemory().(*store.Memory)
	providerID := seedReadyProvider(t, mem, "us-east-1")
	srv := mountMobile(t, mem, nil)

	before := outcomeCount(t, "created")
	resp, body := postMobile(t, srv.URL, map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
		"region":            "us-east-1",
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d body=%s, want 201", resp.StatusCode, body)
	}
	if got := outcomeCount(t, "created") - before; got != 1 {
		t.Errorf("created counter delta=%v, want 1", got)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, body)
	}
	for _, k := range []string{
		"session_id", "peer_public_key", "peer_endpoint",
		"customer_inner_cidr", "allowed_ips", "dns_servers",
		"expires_at", "region", "quota_state",
		// #738: the bare-IP `inner_ip` field the iOS coordinator reads.
		"inner_ip",
	} {
		if _, ok := out[k]; !ok {
			t.Errorf("created response missing field %q (body=%s)", k, body)
		}
	}
	if out["peer_public_key"] != "PROVIDERWGKEY=" {
		t.Errorf("peer_public_key=%v, want PROVIDERWGKEY=", out["peer_public_key"])
	}
	if out["peer_endpoint"] != "203.0.113.7:51820" {
		t.Errorf("peer_endpoint=%v, want 203.0.113.7:51820", out["peer_endpoint"])
	}
	if out["allowed_ips"] != "0.0.0.0/0" {
		t.Errorf("allowed_ips=%v, want 0.0.0.0/0", out["allowed_ips"])
	}

	// #738: inner_ip must be the BARE IP the client reads (coordinator.ts
	// decodes `body.inner_ip`), and it must agree with customer_inner_cidr
	// (the native /32 form). Before the fix the response carried ONLY
	// customer_inner_cidr → the client read undefined → fell back to the
	// hard-coded default 10.66.0.2/32.
	innerIP, _ := out["inner_ip"].(string)
	if innerIP == "" {
		t.Fatalf("inner_ip empty — the client would fall back to the default 10.66.0.2/32 (#738) (body=%s)", body)
	}
	if net.ParseIP(innerIP) == nil || strings.Contains(innerIP, "/") {
		t.Errorf("inner_ip=%q must be a bare IPv4 (no CIDR mask) — the client passes it straight through", innerIP)
	}
	if got, want := out["customer_inner_cidr"], innerIP+"/32"; got != want {
		t.Errorf("customer_inner_cidr=%v, want %q (must agree with inner_ip)", got, want)
	}

	// The session row must exist + be bound to the seeded provider.
	sessID, _ := uuid.Parse(out["session_id"].(string))
	sess, err := mem.GetSession(context.Background(), sessID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.CurrentProvider != providerID {
		t.Errorf("session.CurrentProvider=%v, want %v", sess.CurrentProvider, providerID)
	}
}

// TestRequestMobileSession_NoPeerWhenWgKeyUnpublished covers the #696 path:
// a provider IS selectable AND has a fresh ICE endpoint, but registered
// WITHOUT its static WG public key (a legacy/standalone daemon that omits
// it). lookupProvider must fail safe → 503 + Retry-After, NOT return a 201
// with peer_public_key:"" that configures a dead tunnel which silently
// never handshakes.
func TestRequestMobileSession_NoPeerWhenWgKeyUnpublished(t *testing.T) {
	mem := store.NewMemory().(*store.Memory)
	ctx := context.Background()
	// Provider is healthy + has an srflx endpoint, but NO WG public key.
	pid := uuid.New()
	if err := mem.RegisterProvider(ctx, &store.ProviderInfo{
		ID: pid, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(),
		WgPublicKey: "", // the #696 case: daemon never published its key
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	if err := mem.RegisterCandidates(ctx, pid, []*pb.IceCandidate{{
		Foundation: "1", Transport: "udp", CandidateType: "srflx",
		ConnectionAddress: "203.0.113.7", ConnectionPort: 51820,
		DiscoveredAtUnixMs: time.Now().UnixMilli(),
	}}); err != nil {
		t.Fatalf("register candidates: %v", err)
	}
	srv := mountMobile(t, mem, nil)

	before := outcomeCount(t, "no_peer")
	resp, body := postMobile(t, srv.URL, map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
		"region":            "us-east-1",
	}, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503 (empty WG key must fail safe)", resp.StatusCode, body)
	}
	if got := outcomeCount(t, "no_peer") - before; got != 1 {
		t.Errorf("no_peer counter delta=%v, want 1", got)
	}
}

// TestRequestMobileSession_NoPeerWhenEndpointUnpublished covers the second
// no_peer path: a provider IS selectable (picker succeeds) but has no fresh
// ICE candidate, so lookupProvider fails and the handler must still record
// the `no_peer` outcome + return 503 with Retry-After (#605). This guards
// against a regression where the endpoint-lookup 503 path forgets to
// increment the counter (it would silently never fire the 503 alert).
func TestRequestMobileSession_NoPeerWhenEndpointUnpublished(t *testing.T) {
	mem := store.NewMemory().(*store.Memory)
	// Healthy provider but NO ICE candidates registered → pickEndpoint == "".
	mem.SeedProvider(uuid.New(), "us-east-1", "healthy")
	srv := mountMobile(t, mem, nil)

	before := outcomeCount(t, "no_peer")
	resp, body := postMobile(t, srv.URL, map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
		"region":            "us-east-1",
	}, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503", resp.StatusCode, body)
	}
	if got := outcomeCount(t, "no_peer") - before; got != 1 {
		t.Errorf("no_peer counter delta=%v, want 1 (endpoint-unpublished 503 path must increment)", got)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Errorf("endpoint-unpublished 503 must carry Retry-After header")
	}
}

// TestGetSession_SurfacesInnerIP pins the #738 GET-path fix (issue option b,
// the re-fetch route): after a mobile session is created (which allocates +
// persists the per-session inner IP), GET /v1/vpn/sessions/{id} must surface
// the inner IP so the app can recover it independently of the create
// response. Before the fix the GET handler returned every session field
// EXCEPT the inner IP, so a re-fetch couldn't repair an empty value.
//
// The round-trip here is create-handler → store → GET-handler: the inner IP
// the POST allocated must come back out of GET unchanged. (The Postgres
// store-layer round-trip of InnerIP itself is pinned by the integration test
// TestPostgres_CreateSession_PersistsCustomerWgKey — #726 lesson — so this
// test focuses on the GET handler's serialization of the field.)
func TestGetSession_SurfacesInnerIP(t *testing.T) {
	mem := store.NewMemory().(*store.Memory)
	seedReadyProvider(t, mem, "us-east-1")
	srv := mountMobile(t, mem, nil)

	// 1. Create a mobile session — captures the allocated inner IP.
	resp, body := postMobile(t, srv.URL, map[string]interface{}{
		"customer_id":       uuid.New().String(),
		"client_public_key": "CLIENTPUBKEY1111AAAA2222BBBB=",
		"region":            "us-east-1",
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d body=%s, want 201", resp.StatusCode, body)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create body: %v (%s)", err, body)
	}
	sessionID, _ := created["session_id"].(string)
	wantInnerIP, _ := created["inner_ip"].(string)
	if sessionID == "" || wantInnerIP == "" {
		t.Fatalf("create response missing session_id/inner_ip (body=%s)", body)
	}

	// 2. GET the session back and assert the inner IP surfaces.
	getResp, getBody := getJSON(t, srv.URL+"/v1/vpn/sessions/"+sessionID)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d body=%s, want 200", getResp.StatusCode, getBody)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(getBody, &got); err != nil {
		t.Fatalf("unmarshal GET body: %v (%s)", err, getBody)
	}

	gotInnerIP, _ := got["inner_ip"].(string)
	if gotInnerIP != wantInnerIP {
		t.Errorf("GET inner_ip=%q, want %q (must round-trip create→store→GET, #738)", gotInnerIP, wantInnerIP)
	}
	if net.ParseIP(gotInnerIP) == nil || strings.Contains(gotInnerIP, "/") {
		t.Errorf("GET inner_ip=%q must be a bare IPv4 (no CIDR mask)", gotInnerIP)
	}
	if got, want := got["customer_inner_cidr"], wantInnerIP+"/32"; got != want {
		t.Errorf("GET customer_inner_cidr=%v, want %q", got, want)
	}
}

// getJSON fires a GET against url and returns the response + read body.
func getJSON(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}
