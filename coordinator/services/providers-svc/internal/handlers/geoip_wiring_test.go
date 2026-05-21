package handlers

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1/providersv1connect"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/ca"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/geoip"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

// TestPairDaemon_PopulatesGeoFromXFF verifies the #359 happy path:
// when X-Forwarded-For carries a real public IP and the stub lookuper
// resolves it, the providers row PairDaemon writes carries the country
// + region columns the dashboard renders.
func TestPairDaemon_PopulatesGeoFromXFF(t *testing.T) {
	c, err := ca.NewInMemory()
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	stub := geoip.StubLookuper{ByIP: map[string]geoip.Result{
		"203.0.113.7": {
			CountryCode: "TR",
			CountryName: "Turkey",
			RegionName:  "Istanbul",
			RegionSlug:  "tr-istanbul",
		},
	}}
	h := NewRegistrationHandler(store.NewInMemory(), c, stub, nil)

	ctx := context.Background()
	tok, _ := h.Store.IssuePairingToken(ctx, "11111111-1111-1111-1111-111111111111", 0)
	req := connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok,
		DaemonPublicKey: newDaemonPubKey(t),
		DisplayName:     "Hatices-Mac-mini-2",
	})
	req.Header().Set("X-Forwarded-For", "203.0.113.7, 10.0.0.5")

	resp, err := h.PairDaemon(ctx, req)
	if err != nil {
		t.Fatalf("PairDaemon: %v", err)
	}
	pid := resp.Msg.GetProvider().GetId().GetValue()
	if pid == "" {
		t.Fatal("expected provider id")
	}
	got, err := h.Store.GetProvider(ctx, pid)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if got.NetworkInfo.PublicIP != "203.0.113.7" {
		t.Errorf("PublicIP = %q; want 203.0.113.7", got.NetworkInfo.PublicIP)
	}
	if got.NetworkInfo.CountryCode != "TR" {
		t.Errorf("CountryCode = %q; want TR", got.NetworkInfo.CountryCode)
	}
	if got.NetworkInfo.RegionName != "Istanbul" {
		t.Errorf("RegionName = %q; want Istanbul", got.NetworkInfo.RegionName)
	}
	if got.NetworkInfo.RegionSlug != "tr-istanbul" {
		t.Errorf("RegionSlug = %q; want tr-istanbul", got.NetworkInfo.RegionSlug)
	}
}

// TestPairDaemon_NoopLookuperLeavesGeoBlank ensures pairing still
// succeeds when the .mmdb is unavailable — the geo columns stay empty
// (we never block a pair on a missing geoip DB).
func TestPairDaemon_NoopLookuperLeavesGeoBlank(t *testing.T) {
	h := newTestHandler(t)
	// newTestHandler passes a nil Lookuper which the constructor swaps
	// for NoopLookuper.
	ctx := context.Background()
	tok, _ := h.Store.IssuePairingToken(ctx, "22222222-2222-2222-2222-222222222222", 0)
	req := connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok,
		DaemonPublicKey: newDaemonPubKey(t),
		DisplayName:     "test-daemon",
	})
	req.Header().Set("X-Forwarded-For", "203.0.113.7")
	resp, err := h.PairDaemon(ctx, req)
	if err != nil {
		t.Fatalf("PairDaemon: %v", err)
	}
	pid := resp.Msg.GetProvider().GetId().GetValue()
	got, _ := h.Store.GetProvider(ctx, pid)
	if got.NetworkInfo.CountryCode != "" {
		t.Errorf("CountryCode populated under NoopLookuper: %q", got.NetworkInfo.CountryCode)
	}
	// Public IP, however, IS captured even under the noop — it's a
	// trust-the-observed-IP signal and useful on its own for abuse
	// triage even when we can't geo-resolve it.
	if got.NetworkInfo.PublicIP != "203.0.113.7" {
		t.Errorf("PublicIP = %q; want 203.0.113.7", got.NetworkInfo.PublicIP)
	}
}

// TestPairDaemon_PopulatesGeoFromForwardedRFC7239 verifies the #381
// fallback path: when the upstream proxy emits the standardised
// `Forwarded` (RFC 7239) header instead of (or in addition to) the
// legacy `X-Forwarded-For`, the geoip refresh still resolves the
// provider's country + region. Required so the same provider row
// populates whether the platform is fronted by Traefik (XFF), Cilium
// Gateway (Forwarded), or any forthcoming CDN edge.
func TestPairDaemon_PopulatesGeoFromForwardedRFC7239(t *testing.T) {
	c, err := ca.NewInMemory()
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	stub := geoip.StubLookuper{ByIP: map[string]geoip.Result{
		"203.0.113.7": {
			CountryCode: "TR",
			CountryName: "Turkey",
			RegionName:  "Istanbul",
			RegionSlug:  "tr-istanbul",
		},
	}}
	h := NewRegistrationHandler(store.NewInMemory(), c, stub, nil)

	ctx := context.Background()
	tok, _ := h.Store.IssuePairingToken(ctx, "44444444-4444-4444-4444-444444444444", 0)
	req := connect.NewRequest(&providersv1.PairDaemonRequest{
		PairingToken:    tok,
		DaemonPublicKey: newDaemonPubKey(t),
		DisplayName:     "rfc7239-test",
	})
	// Note: NO X-Forwarded-For. Only the RFC 7239 header.
	req.Header().Set("Forwarded", `for="203.0.113.7:54321";proto=https`)

	resp, err := h.PairDaemon(ctx, req)
	if err != nil {
		t.Fatalf("PairDaemon: %v", err)
	}
	pid := resp.Msg.GetProvider().GetId().GetValue()
	got, err := h.Store.GetProvider(ctx, pid)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if got.NetworkInfo.PublicIP != "203.0.113.7" {
		t.Errorf("PublicIP = %q; want 203.0.113.7 (Forwarded header)", got.NetworkInfo.PublicIP)
	}
	if got.NetworkInfo.CountryCode != "TR" {
		t.Errorf("CountryCode = %q; want TR (Forwarded header)", got.NetworkInfo.CountryCode)
	}
}

// TestPairDaemonREST_ForwardsXFF verifies the REST shim ports the HTTP
// X-Forwarded-For chain onto the in-process Connect request so the
// geoip lookup in PairDaemon sees it. Without this plumbing the
// production REST path (which is what the Rust daemon hits) would
// always skip geo enrichment.
func TestPairDaemonREST_ForwardsXFF(t *testing.T) {
	c, err := ca.NewInMemory()
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	stub := geoip.StubLookuper{ByIP: map[string]geoip.Result{
		"198.51.100.42": {
			CountryCode: "US",
			CountryName: "United States",
			RegionName:  "California",
			RegionSlug:  "us-california",
		},
	}}
	h := NewRegistrationHandler(store.NewInMemory(), c, stub, nil)

	ctx := context.Background()
	tok, _ := h.Store.IssuePairingToken(ctx, "33333333-3333-3333-3333-333333333333", 0)

	// CSR built from the helper used by other rest_pair tests.
	pubKey := newDaemonPubKey(t)
	body := `{"pairing_token":"` + tok + `","daemon_public_key_b64":"` + base64.StdEncoding.EncodeToString(pubKey) + `","display_name":"laptop"}`
	httpReq := httptest.NewRequest("POST", "/api/v1/providers/pair", strings.NewReader(body))
	httpReq.Header.Set("X-Forwarded-For", "198.51.100.42, 10.0.0.5")
	httpReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.PairDaemonREST(rec, httpReq)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Find the row we just created and assert geo was populated.
	ps, _, _ := h.Store.ListProviders(ctx, store.ListOptions{})
	if len(ps) != 1 {
		t.Fatalf("want 1 provider, got %d", len(ps))
	}
	got := ps[0]
	if got.NetworkInfo.CountryCode != "US" {
		t.Errorf("CountryCode = %q; want US", got.NetworkInfo.CountryCode)
	}
	if got.NetworkInfo.RegionSlug != "us-california" {
		t.Errorf("RegionSlug = %q; want us-california", got.NetworkInfo.RegionSlug)
	}
}


// TestStreamHeartbeats_RefreshesGeoFromXFF verifies the #359 heartbeat
// path: when a heartbeat stream arrives with X-Forwarded-For and the
// stub lookuper resolves it, the providers row's geo columns refresh
// even if PairDaemon ran before the .mmdb was loaded. Drives the
// actual Connect handler over h2c so RequestHeader() sees a real
// header set, mirroring TestStreamHeartbeats_BumpsLastSeenAt.
func TestStreamHeartbeats_RefreshesGeoFromXFF(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemory()

	p := &store.Provider{
		OwnerUserID:  "owner-y",
		DisplayName:  "hatice mac mini",
		RegisteredAt: time.Now().Add(-time.Hour).UTC(),
	}
	if err := s.CreateProvider(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stub := geoip.StubLookuper{ByIP: map[string]geoip.Result{
		"203.0.113.7": {
			CountryCode: "TR",
			CountryName: "Turkey",
			RegionName:  "Istanbul",
			RegionSlug:  "tr-istanbul",
		},
	}}
	sched := NewSchedulingHandler(s, stub, nil)
	mux := http.NewServeMux()
	mountPath, handler := providersv1connect.NewSchedulingServiceHandler(sched)
	mux.Handle(mountPath, handler)
	srv := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	srv.EnableHTTP2 = true
	srv.Start()
	defer srv.Close()

	client := providersv1connect.NewSchedulingServiceClient(
		&http.Client{Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}},
		srv.URL,
		connect.WithGRPC(),
	)
	stream := client.StreamHeartbeats(ctx)
	stream.RequestHeader().Set("X-Forwarded-For", "203.0.113.7, 10.0.0.5")

	if err := stream.Send(&providersv1.Heartbeat{
		ProviderId: &commonv1.UUID{Value: p.ID},
		State:      providersv1.SchedulerState_SCHEDULER_STATE_ACTIVE,
		Sequence:   1,
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := stream.Receive(); err != nil {
		t.Fatalf("recv ack: %v", err)
	}
	_ = stream.CloseRequest()
	_ = stream.CloseResponse()

	// Allow the in-handler goroutine (if any) to finish — applyHeartbeatGeo
	// is synchronous inside the receive loop so by the time the ack
	// returns the UPDATE has committed, but give a tiny grace window
	// in case the test runner is loaded.
	deadline := time.Now().Add(time.Second)
	var after *store.Provider
	for time.Now().Before(deadline) {
		after, _ = s.GetProvider(ctx, p.ID)
		if after.NetworkInfo.CountryCode == "TR" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if after.NetworkInfo.CountryCode != "TR" {
		t.Fatalf("CountryCode = %q; want TR (NetworkInfo=%+v)",
			after.NetworkInfo.CountryCode, after.NetworkInfo)
	}
	if after.NetworkInfo.RegionSlug != "tr-istanbul" {
		t.Errorf("RegionSlug = %q; want tr-istanbul", after.NetworkInfo.RegionSlug)
	}
	if after.NetworkInfo.PublicIP != "203.0.113.7" {
		t.Errorf("PublicIP = %q; want 203.0.113.7", after.NetworkInfo.PublicIP)
	}
}

