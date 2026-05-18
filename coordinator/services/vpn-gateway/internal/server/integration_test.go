package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/blocklist"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/customer"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/metering"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/session"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/tier"
	"github.com/iogrid/iogrid/coordinator/services/vpn-gateway/internal/wireguard"
)

func mkPK(b byte) [32]byte {
	var pk [32]byte
	for i := range pk {
		pk[i] = b
	}
	return pk
}

func newTestGW(t *testing.T) (*Gateway, http.Handler) {
	t.Helper()
	bl := blocklist.New()
	if _, err := bl.Load(strings.NewReader("0.0.0.0 ads.example.com\n0.0.0.0 doubleclick.net\n")); err != nil {
		t.Fatalf("blocklist Load: %v", err)
	}
	cr := customer.New()
	// Free user, US, no usage.
	freePK := mkPK(0x11)
	_ = cr.Upsert(customer.Customer{ID: "user-free", PubKey: freePK, AssignedIP: "10.99.0.10", Tier: tier.TierFree, Country: "US"})
	// Plus user, JP.
	plusPK := mkPK(0x22)
	_ = cr.Upsert(customer.Customer{ID: "user-plus", PubKey: plusPK, AssignedIP: "10.99.0.11", Tier: tier.TierPlus, Country: "JP"})
	// Pro user, DE.
	proPK := mkPK(0x33)
	_ = cr.Upsert(customer.Customer{ID: "user-pro", PubKey: proPK, AssignedIP: "10.99.0.12", Tier: tier.TierPro, Country: "DE"})

	g := &Gateway{
		Customers:          cr,
		Blocklist:          bl,
		Meter:              metering.New(nil),
		Sessions:           session.New(0),
		SupportedCountries: []string{"US", "DE", "JP", "GB", "FR", "CA"},
		ServerPublicKeyB64: "ServerPubKey++++++++++++++++++++++++++++++++=",
		ServerEndpoint:     "vpn.iogrid.org:51820",
		DNSAddress:         "10.99.0.1",
	}
	r := chi.NewRouter()
	Mount(g)(r)
	return g, r
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.NewDecoder(rec.Body).Decode(&out)
	}
	return rec.Code, out
}

func TestIndex(t *testing.T) {
	_, h := newTestGW(t)
	code, body := doJSON(t, h, "GET", "/v1/", nil)
	if code != 200 {
		t.Fatalf("/v1/ code = %d", code)
	}
	if body["service"] != "vpn-gateway" {
		t.Errorf("service = %v", body["service"])
	}
}

func TestAdmitFlowFreeOK(t *testing.T) {
	_, h := newTestGW(t)
	pk := mkPK(0x11)
	b64 := stdB64Encode(pk[:])
	code, body := doJSON(t, h, "POST", "/v1/admit", map[string]string{
		"pubkey":  b64,
		"country": "US",
	})
	if code != 200 {
		t.Fatalf("admit code = %d body=%+v", code, body)
	}
	if body["admit"] != true {
		t.Errorf("admit = %v, want true", body["admit"])
	}
	if body["customer_id"] != "user-free" {
		t.Errorf("customer_id = %v", body["customer_id"])
	}
	if body["tier"] != "free" {
		t.Errorf("tier = %v", body["tier"])
	}
	if body["assigned_ip"] != "10.99.0.10" {
		t.Errorf("assigned_ip = %v", body["assigned_ip"])
	}
}

func TestAdmitFreeBlockedByCap(t *testing.T) {
	g, h := newTestGW(t)
	// Push the free user over 2 GB.
	g.Meter.AddBytes("user-free", 2*1024*1024*1024, 0)
	pk := mkPK(0x11)
	code, body := doJSON(t, h, "POST", "/v1/admit", map[string]string{
		"pubkey": stdB64Encode(pk[:]),
	})
	if code != 200 {
		t.Fatalf("admit code = %d", code)
	}
	if body["admit"] != false {
		t.Errorf("admit should be false: %+v", body)
	}
	if body["reason"] != "MONTHLY_CAP_EXCEEDED" {
		t.Errorf("reason = %v", body["reason"])
	}
	if body["over_monthly_cap"] != true {
		t.Error("over_monthly_cap flag missing")
	}
}

func TestAdmitUnknownPeer(t *testing.T) {
	_, h := newTestGW(t)
	pk := mkPK(0xfe) // not registered
	code, body := doJSON(t, h, "POST", "/v1/admit", map[string]string{
		"pubkey": stdB64Encode(pk[:]),
	})
	if code != 200 {
		t.Fatalf("admit code = %d", code)
	}
	if body["admit"] != false {
		t.Error("unknown peer must be rejected")
	}
	if body["reason"] != "UNKNOWN_PEER" {
		t.Errorf("reason = %v", body["reason"])
	}
}

func TestAdmitPlusCountryAllowed(t *testing.T) {
	_, h := newTestGW(t)
	pk := mkPK(0x22) // plus user
	code, body := doJSON(t, h, "POST", "/v1/admit", map[string]string{
		"pubkey":  stdB64Encode(pk[:]),
		"country": "DE",
	})
	if code != 200 {
		t.Fatalf("admit code = %d", code)
	}
	if body["admit"] != true {
		t.Errorf("plus DE should be allowed: %+v", body)
	}
	if body["provider_id"] != "provider-de" {
		t.Errorf("provider_id = %v, want provider-de", body["provider_id"])
	}
}

func TestAdmitPlusCountryDenied(t *testing.T) {
	_, h := newTestGW(t)
	pk := mkPK(0x22) // plus user
	code, body := doJSON(t, h, "POST", "/v1/admit", map[string]string{
		"pubkey":  stdB64Encode(pk[:]),
		"country": "ZZ",
	})
	if code != 200 {
		t.Fatalf("admit code = %d", code)
	}
	if body["admit"] != false {
		t.Error("unsupported country must be rejected")
	}
	if body["reason"] != "UNSUPPORTED_COUNTRY" {
		t.Errorf("reason = %v", body["reason"])
	}
}

func TestDNSResolveProBlocks(t *testing.T) {
	_, h := newTestGW(t)
	code, body := doJSON(t, h, "GET", "/v1/dns/resolve?host=ads.example.com&customer_id=user-pro", nil)
	if code != 200 {
		t.Fatalf("dns code = %d", code)
	}
	if body["blocked"] != true {
		t.Error("pro tier should block ads.example.com")
	}
	if body["tier"] != "pro" {
		t.Errorf("tier = %v", body["tier"])
	}
}

func TestDNSResolveFreePassesThrough(t *testing.T) {
	_, h := newTestGW(t)
	code, body := doJSON(t, h, "GET", "/v1/dns/resolve?host=ads.example.com&customer_id=user-free", nil)
	if code != 200 {
		t.Fatalf("dns code = %d", code)
	}
	if body["blocked"] != false {
		t.Error("free tier should NOT block — ad-block is Pro only")
	}
}

func TestDNSResolveProAllowsCleanHost(t *testing.T) {
	_, h := newTestGW(t)
	code, body := doJSON(t, h, "GET", "/v1/dns/resolve?host=openova.io&customer_id=user-pro", nil)
	if code != 200 {
		t.Fatalf("dns code = %d", code)
	}
	if body["blocked"] != false {
		t.Error("clean host should not be blocked")
	}
}

func TestPeerStatsEndpoint(t *testing.T) {
	g, h := newTestGW(t)
	g.Meter.AddBytes("user-plus", 500, 1500)
	pk := mkPK(0x22)
	code, body := doJSON(t, h, "GET", "/v1/peers/"+stdB64Encode(pk[:])+"/stats", nil)
	if code != 200 {
		t.Fatalf("stats code = %d body=%+v", code, body)
	}
	if int(body["bytes_in"].(float64)) != 500 {
		t.Errorf("bytes_in = %v", body["bytes_in"])
	}
	if int(body["bytes_out"].(float64)) != 1500 {
		t.Errorf("bytes_out = %v", body["bytes_out"])
	}
}

func TestRenderConfigEndpoint(t *testing.T) {
	_, h := newTestGW(t)
	req := httptest.NewRequest("POST", "/v1/config/render", strings.NewReader(`{"customer_id":"user-plus","platform":"linux","customer_private_key":"client-priv-key="}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("render code = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "[Interface]") || !strings.Contains(body, "[Peer]") {
		t.Errorf("config missing sections: %s", body)
	}
	if !strings.Contains(body, "vpn.iogrid.org:51820") {
		t.Errorf("endpoint missing")
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), ".conf") {
		t.Errorf("missing content-disposition: %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestRenderConfigIOSMobileconfig(t *testing.T) {
	_, h := newTestGW(t)
	req := httptest.NewRequest("POST", "/v1/config/render", strings.NewReader(`{"customer_id":"user-pro","platform":"ios"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("render code = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "com.wireguard.macos") {
		t.Error("ios config should be mobileconfig with wireguard subtype")
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), ".mobileconfig") {
		t.Errorf("filename = %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestRenderConfigUnknownCustomer(t *testing.T) {
	_, h := newTestGW(t)
	req := httptest.NewRequest("POST", "/v1/config/render", strings.NewReader(`{"customer_id":"ghost","platform":"linux"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// stdB64Encode duplicates a tiny helper here to keep the test file
// self-contained — we don't want a circular dep on customer.stdB64.
func stdB64Encode(b []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	// Minimal std-b64 encoder so the tests don't pull encoding/base64
	// into this file directly. (We use it indirectly via customer.DecodePubKey.)
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		var x uint32
		var pad int
		x |= uint32(b[i]) << 16
		if i+1 < len(b) {
			x |= uint32(b[i+1]) << 8
		} else {
			pad++
		}
		if i+2 < len(b) {
			x |= uint32(b[i+2])
		} else {
			pad++
		}
		out.WriteByte(alphabet[(x>>18)&0x3f])
		out.WriteByte(alphabet[(x>>12)&0x3f])
		if pad < 2 {
			out.WriteByte(alphabet[(x>>6)&0x3f])
		} else {
			out.WriteByte('=')
		}
		if pad < 1 {
			out.WriteByte(alphabet[x&0x3f])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
}

// TestWireGuardMockRoundTrip verifies the WG mock (the same code path
// tests use to simulate the data plane) interacts correctly with the
// gateway's peer-stats endpoint.
func TestWireGuardMockRoundTrip(t *testing.T) {
	g, h := newTestGW(t)
	wg := wireguard.NewMock()
	ctx := context.Background()
	addr, _ := net.ResolveUDPAddr("udp", ":51820")
	var priv [32]byte
	priv[0] = 0x77
	if err := wg.Start(ctx, addr, priv); err != nil {
		t.Fatalf("wg Start: %v", err)
	}

	pk := mkPK(0x22)
	ip := net.ParseIP("10.99.0.11")
	if err := wg.AddPeer(ctx, pk, ip, nil); err != nil {
		t.Fatalf("wg AddPeer: %v", err)
	}
	wg.SimulateTraffic(pk, 1024, 4096)
	stats, err := wg.PeerStats(ctx, pk)
	if err != nil {
		t.Fatalf("wg PeerStats: %v", err)
	}
	// Push the WG counters into the gateway meter (the data plane does
	// this on a flush cadence).
	g.Meter.AddBytes("user-plus", stats.BytesReceived, stats.BytesSent)
	code, body := doJSON(t, h, "GET", "/v1/peers/"+stdB64Encode(pk[:])+"/stats", nil)
	if code != 200 {
		t.Fatalf("stats code = %d", code)
	}
	if int(body["bytes_total"].(float64)) != 1024+4096 {
		t.Errorf("bytes_total = %v, want %d", body["bytes_total"], 1024+4096)
	}
}
