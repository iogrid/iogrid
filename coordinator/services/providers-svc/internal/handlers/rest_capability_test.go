package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

// postCapabilities is a small helper that drives CapabilityReportREST with
// the chi {id} URL param wired up (httptest.NewRequest doesn't run the
// router, so the param has to be injected into the request context).
func postCapabilities(t *testing.T, h *RegistrationHandler, providerID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/providers/"+providerID+"/capabilities", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", providerID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.CapabilityReportREST(rr, req)
	return rr
}

// #746: a Mac that paired BEFORE Xcode was installed has an empty
// capability record. Its startup capability POST must flip
// ios_build_enabled=true + populate supported_types + host_macos_version,
// so the admin / provider dashboard stops under-reporting it.
func TestCapabilityReportREST_RefreshesStaleRecord(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	// A provider paired with NO capabilities (the real post-pairing state).
	p := &store.Provider{
		ID:          "c0138910-9f41-4a05-972f-c6915760e0f0",
		OwnerUserID: "11111111-1111-1111-1111-111111111111",
		DisplayName: "Hatices-Mac-mini-2",
		Capabilities: store.Capability{
			SupportedTypes: []string{}, // empty == stale
		},
	}
	if err := h.Store.CreateProvider(ctx, p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}

	rr := postCapabilities(t, h, p.ID, map[string]any{
		"supported_types":    []string{"BANDWIDTH", "IOS_BUILD"},
		"gpu_enabled":        false,
		"ios_build_enabled":  true,
		"host_macos_version": 14,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	got, err := h.Store.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	if !got.Capabilities.IOSBuildEnabled {
		t.Fatal("ios_build_enabled must be true after capability report")
	}
	if got.Capabilities.HostMacosVersion != 14 {
		t.Fatalf("host_macos_version = %d, want 14", got.Capabilities.HostMacosVersion)
	}
	wantTypes := map[string]bool{"bandwidth": true, "ios_build": true}
	if len(got.Capabilities.SupportedTypes) != len(wantTypes) {
		t.Fatalf("supported_types = %v, want bandwidth+ios_build", got.Capabilities.SupportedTypes)
	}
	for _, s := range got.Capabilities.SupportedTypes {
		if !wantTypes[s] {
			t.Fatalf("unexpected supported type %q in %v", s, got.Capabilities.SupportedTypes)
		}
	}
}

// An unknown provider id must 404 (the in-process UpdateCapabilityInventory
// loads the row first), not 500 or a silent no-op.
func TestCapabilityReportREST_UnknownProvider404(t *testing.T) {
	h := newTestHandler(t)
	rr := postCapabilities(t, h, "00000000-0000-0000-0000-0000000000ff", map[string]any{
		"supported_types":   []string{"BANDWIDTH"},
		"ios_build_enabled": false,
	})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: want 404 for unknown provider, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// Malformed JSON (or an unknown field) must 400 with the {"error":...}
// envelope, matching the pairing shim's contract.
func TestCapabilityReportREST_MalformedJSON400(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/providers/abc/capabilities", bytes.NewReader([]byte("{not json")))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.CapabilityReportREST(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rr.Code)
	}
}
