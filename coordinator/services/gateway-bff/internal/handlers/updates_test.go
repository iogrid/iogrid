package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
)

func TestGetUpdates_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/account/updates", nil)
	w := httptest.NewRecorder()
	api.GetUpdates(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestGetUpdates_ReturnsDefaults(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/account/updates", nil))
	w := httptest.NewRecorder()
	api.GetUpdates(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Preferences UpdatePreferences `json:"preferences"`
		State       UpdateState       `json:"state"`
	}
	mustReadJSON(t, w.Body, &resp)
	if resp.Preferences.Channel != "stable" {
		t.Fatalf("default channel = %q, want stable", resp.Preferences.Channel)
	}
	if resp.Preferences.AutoUpdate {
		t.Fatalf("default autoUpdate should be false")
	}
	if resp.State.History == nil {
		t.Fatalf("history must be non-nil slice")
	}
}

func TestSaveUpdatePreferences_PersistsAndReturns(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	body, _ := json.Marshal(UpdatePreferences{Channel: "beta", AutoUpdate: true})
	r := withAuth(httptest.NewRequest(http.MethodPost,
		"/api/v1/account/updates/preferences", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.SaveUpdatePreferences(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", w.Code, w.Body.String())
	}
	// Round-trip GET to confirm persistence.
	r2 := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/account/updates", nil))
	w2 := httptest.NewRecorder()
	api.GetUpdates(w2, r2)
	var resp struct {
		Preferences UpdatePreferences `json:"preferences"`
	}
	mustReadJSON(t, w2.Body, &resp)
	if resp.Preferences.Channel != "beta" || !resp.Preferences.AutoUpdate {
		t.Fatalf("preferences not persisted: %+v", resp.Preferences)
	}
}

func TestSaveUpdatePreferences_RejectsUnknownChannel(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	body, _ := json.Marshal(map[string]any{"channel": "nightly", "autoUpdate": false})
	r := withAuth(httptest.NewRequest(http.MethodPost,
		"/api/v1/account/updates/preferences", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.SaveUpdatePreferences(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSaveUpdatePreferences_AcceptsCanary(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	for _, ch := range []string{"stable", "beta", "canary", "STABLE", "  beta "} {
		body, _ := json.Marshal(map[string]any{"channel": ch, "autoUpdate": false})
		r := withAuth(httptest.NewRequest(http.MethodPost,
			"/api/v1/account/updates/preferences", bytes.NewReader(body)))
		w := httptest.NewRecorder()
		api.SaveUpdatePreferences(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("want 200 for %q, got %d", ch, w.Code)
		}
	}
}

func TestTriggerCheck_RecordsHistory(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	rc := withAuth(httptest.NewRequest(http.MethodPost,
		"/api/v1/account/updates/check", nil))
	wc := httptest.NewRecorder()
	api.TriggerUpdateCheck(wc, rc)
	if wc.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", wc.Code)
	}
	// GET should now have one history entry.
	rg := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/account/updates", nil))
	wg := httptest.NewRecorder()
	api.GetUpdates(wg, rg)
	var resp struct {
		State UpdateState `json:"state"`
	}
	mustReadJSON(t, wg.Body, &resp)
	if len(resp.State.History) != 1 {
		t.Fatalf("history len = %d, want 1", len(resp.State.History))
	}
	if status, _ := resp.State.History[0].Outcome["status"].(string); status != "up_to_date" {
		t.Fatalf("outcome.status = %q, want up_to_date", status)
	}
}

func TestApplyAndRollback_RequireAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	for _, fn := range []http.HandlerFunc{api.ApplyPendingUpdate, api.RollbackUpdate} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/account/updates/x", nil)
		w := httptest.NewRecorder()
		fn(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d", w.Code)
		}
	}
}

func TestApplyAndRollback_HappyPath(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	for _, fn := range []http.HandlerFunc{api.ApplyPendingUpdate, api.RollbackUpdate} {
		r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/account/updates/x", nil))
		w := httptest.NewRecorder()
		fn(w, r)
		if w.Code != http.StatusAccepted {
			t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestUpdatesStore_HistoryCapAt50(t *testing.T) {
	s := newUpdatesStore()
	for i := 0; i < 60; i++ {
		s.appendHistory("u", UpdateHistoryEntry{
			At:          "2026-05-19T00:00:00Z",
			Channel:     "stable",
			FromVersion: "0.1.0",
			Outcome:     UpdateOutcome{"status": "up_to_date", "current": "0.1.0"},
		})
	}
	st := s.getState("u")
	if len(st.History) != 50 {
		t.Fatalf("history len = %d, want 50", len(st.History))
	}
}

func TestValidateChannel(t *testing.T) {
	for _, ok := range []string{"stable", "beta", "canary", "STABLE", " beta "} {
		if err := validateChannel(ok); err != nil {
			t.Fatalf("want ok for %q, got %v", ok, err)
		}
	}
	for _, bad := range []string{"", "nightly", "edge-x", "rc"} {
		if err := validateChannel(bad); err == nil {
			t.Fatalf("want err for %q", bad)
		}
	}
}
