package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The status proxy (#674) must re-publish telemetry-svc's pinned paths
// verbatim, stay public, and fail fast (503/502) instead of hanging the
// status page.

func TestStatusPosture_ProxiesUpstreamVerbatim(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status/posture" {
			t.Errorf("upstream path = %q, want /status/posture", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema_version":1,"overall":{"status":"up"}}`))
	}))
	defer upstream.Close()

	api := New(nil, nil, nil)
	api.TelemetrySvcURL = upstream.URL

	w := httptest.NewRecorder()
	api.StatusPosture(w, httptest.NewRequest(http.MethodGet, "/status/posture", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"schema_version":1`) {
		t.Fatalf("body not proxied verbatim: %s", w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age=15") {
		t.Fatalf("Cache-Control = %q, want short shared cache", cc)
	}
}

func TestStatusUptime_UnconfiguredReturns503(t *testing.T) {
	api := New(nil, nil, nil)
	api.TelemetrySvcURL = ""

	w := httptest.NewRecorder()
	api.StatusUptime(w, httptest.NewRequest(http.MethodGet, "/status/uptime", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", w.Code)
	}
}

func TestStatusPosture_UpstreamDownReturns502(t *testing.T) {
	api := New(nil, nil, nil)
	api.TelemetrySvcURL = "http://127.0.0.1:1" // nothing listens here

	w := httptest.NewRecorder()
	api.StatusPosture(w, httptest.NewRequest(http.MethodGet, "/status/posture", nil))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502", w.Code)
	}
}

func TestStatusPosture_UpstreamErrorCodePassesThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"posture generator broken"}`))
	}))
	defer upstream.Close()

	api := New(nil, nil, nil)
	api.TelemetrySvcURL = upstream.URL

	w := httptest.NewRecorder()
	api.StatusPosture(w, httptest.NewRequest(http.MethodGet, "/status/posture", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want upstream 500 passed through", w.Code)
	}
}

func TestStatusUptime_ForwardsValidatedServiceParam(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		_, _ = w.Write([]byte(`{"days":90,"samples":[]}`))
	}))
	defer upstream.Close()

	api := New(nil, nil, nil)
	api.TelemetrySvcURL = upstream.URL

	w := httptest.NewRecorder()
	api.StatusUptime(w, httptest.NewRequest(http.MethodGet, "/status/uptime?service=identity-svc", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if gotPath != "/status/uptime?service=identity-svc" {
		t.Fatalf("upstream path = %q, want the validated service param forwarded", gotPath)
	}
}

func TestStatusUptime_RejectsHostileServiceParam(t *testing.T) {
	api := New(nil, nil, nil)
	api.TelemetrySvcURL = "http://example.invalid" // must never be reached

	for _, bad := range []string{"../../etc", "a b", "x?y=1", strings.Repeat("a", 41), "UPPER"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/status/uptime", nil)
		q := req.URL.Query()
		q.Set("service", bad)
		req.URL.RawQuery = q.Encode()
		api.StatusUptime(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("service=%q: code = %d, want 400", bad, w.Code)
		}
	}
}
