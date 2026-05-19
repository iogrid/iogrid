package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeVPNGateway is a minimal httptest server impersonating the
// vpn-gateway control plane. It echoes a fake .conf body for known
// customers and 404s the rest.
func newFakeVPNGateway(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/config/render" {
			http.Error(w, "not found", 404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "\"customer_id\":\"ghost\"") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"unknown customer"}`))
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\"iogrid-vpn-test.conf\"")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("[Interface]\nPrivateKey =\nAddress = 10.99.0.7/32\n[Peer]\nEndpoint = vpn.iogrid.org:51820\n"))
	}))
}

func newAPIWithVPN(srv *httptest.Server) *API {
	return &API{
		Logger:     slog.Default(),
		VPNGateway: NewVPNGatewayProxy(srv.URL),
	}
}

func TestVPNConfigForPlatform_Success(t *testing.T) {
	srv := newFakeVPNGateway(t)
	defer srv.Close()
	api := newAPIWithVPN(srv)

	req := httptest.NewRequest("GET", "/api/v1/vpn/config-for-platform?customer_id=u1&platform=linux", nil)
	rec := httptest.NewRecorder()
	api.GetVPNConfigForPlatform(rec, req)

	if rec.Code != 200 {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "[Interface]") {
		t.Errorf("body should contain wg-quick config: %s", body)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), ".conf") {
		t.Errorf("Content-Disposition = %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestVPNConfigForPlatform_MissingParams(t *testing.T) {
	srv := newFakeVPNGateway(t)
	defer srv.Close()
	api := newAPIWithVPN(srv)

	for _, q := range []string{
		"customer_id=u1", // missing platform
		"platform=linux", // missing customer_id
	} {
		req := httptest.NewRequest("GET", "/api/v1/vpn/config-for-platform?"+q, nil)
		rec := httptest.NewRecorder()
		api.GetVPNConfigForPlatform(rec, req)
		if rec.Code != 400 {
			t.Errorf("query %q expected 400, got %d", q, rec.Code)
		}
	}
}

func TestVPNConfigForPlatform_UnsupportedPlatform(t *testing.T) {
	srv := newFakeVPNGateway(t)
	defer srv.Close()
	api := newAPIWithVPN(srv)

	req := httptest.NewRequest("GET", "/api/v1/vpn/config-for-platform?customer_id=u1&platform=blackberry", nil)
	rec := httptest.NewRecorder()
	api.GetVPNConfigForPlatform(rec, req)
	if rec.Code != 400 {
		t.Errorf("blackberry should 400, got %d", rec.Code)
	}
}

func TestVPNConfigForPlatform_UpstreamError(t *testing.T) {
	srv := newFakeVPNGateway(t)
	defer srv.Close()
	api := newAPIWithVPN(srv)

	req := httptest.NewRequest("GET", "/api/v1/vpn/config-for-platform?customer_id=ghost&platform=linux", nil)
	rec := httptest.NewRecorder()
	api.GetVPNConfigForPlatform(rec, req)
	if rec.Code != 404 {
		t.Errorf("ghost customer should pass-through 404, got %d", rec.Code)
	}
}

func TestVPNConfigForPlatform_Unconfigured(t *testing.T) {
	api := &API{Logger: slog.Default()} // VPNGateway nil
	req := httptest.NewRequest("GET", "/api/v1/vpn/config-for-platform?customer_id=u1&platform=linux", nil)
	rec := httptest.NewRecorder()
	api.GetVPNConfigForPlatform(rec, req)
	if rec.Code != 503 {
		t.Errorf("no vpn-gateway should 503, got %d", rec.Code)
	}
}
