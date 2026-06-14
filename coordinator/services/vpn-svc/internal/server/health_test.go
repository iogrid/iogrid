// VPN-7 (#511): provider daemon health probes + graceful offline.
//
// Drives the new handlers added in handlers.go for
// `POST /v1/vpn/providers/{providerID}/health` and `.../offline`.
// Tests use the in-memory Store seeded via the new
// `Store.RegisterProvider` helper.

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

func seedProvider(t *testing.T, st store.Store, region string) uuid.UUID {
	t.Helper()
	providerID := uuid.New()
	if err := st.RegisterProvider(context.Background(), &store.ProviderInfo{
		ID:         providerID,
		Region:     region,
		Status:     "healthy",
		LastSeenAt: time.Now().Add(-1 * time.Hour),
	}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return providerID
}

func buildRequest(t *testing.T, method, path string, providerID uuid.UUID, body any) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	r := httptest.NewRequest(method, path, reader)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("providerID", providerID.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	return r
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestUpdateHealth_HappyPath(t *testing.T) {
	st := store.NewMemory()
	providerID := seedProvider(t, st, "us-east-1")
	handler := NewUpdateHealth(st, discardLogger())

	beforeTs := time.Now().Add(-30 * time.Second).UnixMilli()
	body := map[string]any{
		"provider_id":     providerID.String(),
		"status":          "healthy",
		"at_unix_ms":      beforeTs,
		"vpn_listen_addr": "203.0.113.5:51820",
	}
	r := buildRequest(t, http.MethodPost, "/v1/vpn/providers/"+providerID.String()+"/health", providerID, body)
	w := httptest.NewRecorder()
	handler.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got, err := st.GetProvidersInRegion(context.Background(), "us-east-1")
	if err != nil {
		t.Fatalf("get providers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(got))
	}
	if got[0].Status != "healthy" {
		t.Errorf("expected status healthy, got %s", got[0].Status)
	}
	if got[0].LastSeenAt.UnixMilli() != beforeTs {
		t.Errorf("expected LastSeenAt to match at_unix_ms %d, got %d",
			beforeTs, got[0].LastSeenAt.UnixMilli())
	}
}

func TestUpdateHealth_DegradedAccepted(t *testing.T) {
	st := store.NewMemory()
	providerID := seedProvider(t, st, "us-east-1")
	handler := NewUpdateHealth(st, discardLogger())

	body := map[string]any{
		"provider_id": providerID.String(),
		"status":      "degraded",
		"at_unix_ms":  time.Now().UnixMilli(),
	}
	r := buildRequest(t, http.MethodPost, "/v1/vpn/providers/"+providerID.String()+"/health", providerID, body)
	w := httptest.NewRecorder()
	handler.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateHealth_RejectsBadStatus(t *testing.T) {
	st := store.NewMemory()
	providerID := seedProvider(t, st, "us-east-1")
	handler := NewUpdateHealth(st, discardLogger())

	// `/health` only accepts healthy + degraded — `offline` must go
	// through the dedicated `/offline` endpoint so dashboards can
	// distinguish a graceful shutdown from a degraded heartbeat.
	for _, badStatus := range []string{"offline", "broken", "OK", ""} {
		body := map[string]any{
			"provider_id": providerID.String(),
			"status":      badStatus,
			"at_unix_ms":  time.Now().UnixMilli(),
		}
		r := buildRequest(t, http.MethodPost, "/v1/vpn/providers/"+providerID.String()+"/health", providerID, body)
		w := httptest.NewRecorder()
		handler.Handle(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status=%q expected 400, got %d", badStatus, w.Code)
		}
	}
}

func TestUpdateHealth_UnknownProviderReturns404(t *testing.T) {
	st := store.NewMemory()
	handler := NewUpdateHealth(st, discardLogger())

	providerID := uuid.New() // never registered
	body := map[string]any{
		"provider_id": providerID.String(),
		"status":      "healthy",
		"at_unix_ms":  time.Now().UnixMilli(),
	}
	r := buildRequest(t, http.MethodPost, "/v1/vpn/providers/"+providerID.String()+"/health", providerID, body)
	w := httptest.NewRecorder()
	handler.Handle(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMarkOffline_ExcludesProviderFromHealthyRegionList(t *testing.T) {
	st := store.NewMemory()
	providerID := seedProvider(t, st, "us-east-1")
	handler := NewMarkOffline(st, discardLogger())

	// Sanity: provider is in the healthy list to start with.
	healthy, err := st.GetProvidersInRegion(context.Background(), "us-east-1")
	if err != nil || len(healthy) != 1 {
		t.Fatalf("seed mismatch: %d providers (err=%v)", len(healthy), err)
	}

	shutdownTs := time.Now().UnixMilli()
	body := map[string]any{
		"provider_id": providerID.String(),
		"at_unix_ms":  shutdownTs,
		"reason":      "sigterm",
	}
	r := buildRequest(t, http.MethodPost, "/v1/vpn/providers/"+providerID.String()+"/offline", providerID, body)
	w := httptest.NewRecorder()
	handler.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// `GetProvidersInRegion` excludes status=offline rows — flipping
	// the row to offline should drop it from the healthy region list,
	// which is exactly what the customer SDK's failover detector reads.
	after, err := st.GetProvidersInRegion(context.Background(), "us-east-1")
	if err != nil {
		t.Fatalf("get providers: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected provider excluded from healthy region list after offline, got %d", len(after))
	}
}

func TestMarkOffline_EmptyBodyAccepted(t *testing.T) {
	st := store.NewMemory()
	providerID := seedProvider(t, st, "us-east-1")
	handler := NewMarkOffline(st, discardLogger())

	// A hard SIGTERM mid-write may produce an empty body — handler
	// must tolerate it and substitute server-side `now` for the
	// LastSeenAt stamp.
	r := buildRequest(t, http.MethodPost, "/v1/vpn/providers/"+providerID.String()+"/offline", providerID, nil)
	w := httptest.NewRecorder()
	handler.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on empty body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegisterProvider_Idempotent(t *testing.T) {
	// The new Store.RegisterProvider is idempotent — re-registering
	// the same id replaces region/status/last_seen but doesn't bump
	// SessionCount.
	st := store.NewMemory()
	id := uuid.New()
	first := &store.ProviderInfo{
		ID:           id,
		Region:       "us-east-1",
		Status:       "healthy",
		LastSeenAt:   time.Now(),
		SessionCount: 3,
	}
	if err := st.RegisterProvider(context.Background(), first); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Re-register with different region — should overwrite region but
	// leave SessionCount alone (the idempotent contract per docstring).
	second := &store.ProviderInfo{
		ID:         id,
		Region:     "eu-west-1",
		Status:     "degraded",
		LastSeenAt: time.Now(),
	}
	if err := st.RegisterProvider(context.Background(), second); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	got, _ := st.GetProvidersInRegion(context.Background(), "eu-west-1")
	if len(got) != 1 {
		t.Fatalf("expected provider in new region, got %d", len(got))
	}
	if got[0].Status != "degraded" {
		t.Errorf("expected status degraded after re-register, got %s", got[0].Status)
	}
	if got[0].SessionCount != 3 {
		t.Errorf("expected SessionCount preserved at 3, got %d", got[0].SessionCount)
	}
}

// TestRegisterProvider_ServerKeyChangeInvalidatesBoundSessions is the #762
// end-to-end guard at the handler level: a daemon that re-provisions onto an
// empty state-dir re-registers with a NEW WG server pubkey. The register
// handler must (a) still 200, AND (b) terminate the still-active sessions
// bound to that provider so each client reconnects and re-fetches the new key
// (instead of MAC1-rejecting every handshake against the old baked key forever).
func TestRegisterProvider_ServerKeyChangeInvalidatesBoundSessions(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	providerID := uuid.New()

	// Provider already registered with its original key.
	if err := st.RegisterProvider(ctx, &store.ProviderInfo{
		ID: providerID, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(), WgPublicKey: "OLDSERVERKEY",
	}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	// An active session bound to that provider (a client already connected).
	sessionID := uuid.New()
	if err := st.CreateSession(ctx, &store.Session{
		ID:                  sessionID,
		CustomerID:          uuid.New(),
		Region:              "us-east-1",
		PrimaryProvider:     providerID,
		CurrentProvider:     providerID,
		ProviderWgPublicKey: "OLDSERVERKEY",
		CreatedAt:           time.Now(),
		LastActivityAt:      time.Now(),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	handler := NewRegisterProvider(st, discardLogger())
	body := map[string]any{"region": "us-east-1", "wg_public_key": "NEWSERVERKEY"}
	r := buildRequest(t, http.MethodPost, "/v1/vpn/providers/"+providerID.String()+"/register", providerID, body)
	w := httptest.NewRecorder()
	handler.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on re-register, got %d: %s", w.Code, w.Body.String())
	}
	// The bound session must now be terminated so the client reconnects.
	got, err := st.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.TerminatedAt == nil {
		t.Errorf("bound session must be terminated after server-key rotation (else client stays stranded on old key)")
	}
	if got.ExitReason != "provider_key_rotated" {
		t.Errorf("exit reason = %q, want provider_key_rotated", got.ExitReason)
	}
	// The provider row must hold the NEW key so the next mobile bring-up
	// hands clients the rotated peer_public_key.
	probes, _ := st.SelectTopProvidersInRegion(ctx, "us-east-1", 1)
	if len(probes) != 1 || probes[0].WgPublicKey != "NEWSERVERKEY" {
		t.Errorf("provider key not rotated to NEWSERVERKEY after register: %+v", probes)
	}
}

// TestRegisterProvider_SameKeyKeepsSessions guards the steady-state path: a
// normal heartbeat-style re-register (same key — the daemon never rotated)
// must NOT terminate live sessions. Without this the binder's own register
// calls would nuke every connection on each restart.
func TestRegisterProvider_SameKeyKeepsSessions(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	providerID := uuid.New()
	if err := st.RegisterProvider(ctx, &store.ProviderInfo{
		ID: providerID, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(), WgPublicKey: "STABLEKEY",
	}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	sessionID := uuid.New()
	if err := st.CreateSession(ctx, &store.Session{
		ID: sessionID, CustomerID: uuid.New(), Region: "us-east-1",
		PrimaryProvider: providerID, CurrentProvider: providerID,
		CreatedAt: time.Now(), LastActivityAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	handler := NewRegisterProvider(st, discardLogger())
	body := map[string]any{"region": "us-east-1", "wg_public_key": "STABLEKEY"}
	r := buildRequest(t, http.MethodPost, "/v1/vpn/providers/"+providerID.String()+"/register", providerID, body)
	w := httptest.NewRecorder()
	handler.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got, _ := st.GetSession(ctx, sessionID)
	if got.TerminatedAt != nil {
		t.Errorf("same-key re-register must NOT terminate live sessions (steady-state heartbeat)")
	}
}
