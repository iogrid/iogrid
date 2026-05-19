package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
)

// --- store-level invariants ---------------------------------------------

func TestMemoryOnboardStore_LinkAndDefaultsRoundTrip(t *testing.T) {
	s := NewMemoryOnboardStore()
	code := "ABC123"
	if err := s.Mint(code); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if err := s.Link(code, "user-1"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	uid, ok := s.GetLinkedUser(code)
	if !ok || uid != "user-1" {
		t.Fatalf("GetLinkedUser: ok=%v uid=%q", ok, uid)
	}

	d := OnboardingDefaults{
		BandwidthCapGB: 50,
		CPUCapPct:      30,
		IdleOnly:       true,
		Categories:     []string{"ecommerce"},
		PayoutTier:     "defer",
	}
	if err := s.StoreDefaults(code, d); err != nil {
		t.Fatalf("StoreDefaults: %v", err)
	}
	got, ok := s.GetDefaults(code)
	if !ok || got.BandwidthCapGB != 50 || got.PayoutTier != "defer" {
		t.Fatalf("GetDefaults mismatch: %#v", got)
	}

	s.Burn(code)
	if _, ok := s.GetLinkedUser(code); ok {
		t.Fatal("Burn should have removed the entry")
	}
}

func TestMemoryOnboardStore_LinkRejectsConflict(t *testing.T) {
	s := NewMemoryOnboardStore()
	code := "ABC123"
	_ = s.Mint(code)
	if err := s.Link(code, "user-1"); err != nil {
		t.Fatalf("first link: %v", err)
	}
	err := s.Link(code, "user-2")
	if err != ErrCodeAlreadyClaimed {
		t.Fatalf("expected ErrCodeAlreadyClaimed, got %v", err)
	}
	// Same user re-linking is idempotent.
	if err := s.Link(code, "user-1"); err != nil {
		t.Fatalf("idempotent re-link: %v", err)
	}
}

func TestMemoryOnboardStore_StoreDefaultsRequiresLink(t *testing.T) {
	s := NewMemoryOnboardStore()
	_ = s.Mint("ABC123")
	err := s.StoreDefaults("ABC123", OnboardingDefaults{})
	if err == nil {
		t.Fatal("expected error storing defaults on unlinked code")
	}
}

// --- handler tests ------------------------------------------------------

// startReq builds a JSON request body { token: x }.
func startReq(t *testing.T, code string) *http.Request {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": code})
	return httptest.NewRequest(http.MethodPost, "/api/v1/onboard/start", bytes.NewReader(body))
}

func TestStartOnboard_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{}).WithOnboardStore(NewMemoryOnboardStore())
	w := httptest.NewRecorder()
	api.StartOnboard(w, startReq(t, "ABC123"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestStartOnboard_RejectsMalformedToken(t *testing.T) {
	api := newAPI(t, &clients.Set{}).WithOnboardStore(NewMemoryOnboardStore())
	r := withAuth(startReq(t, "lower-case-bad"))
	w := httptest.NewRecorder()
	api.StartOnboard(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStartOnboard_HappyPath_LinksToken(t *testing.T) {
	store := NewMemoryOnboardStore()
	_ = store.Mint("ABC123")
	api := newAPI(t, &clients.Set{}).WithOnboardStore(store)
	r := withAuth(startReq(t, "ABC123"))
	w := httptest.NewRecorder()
	api.StartOnboard(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	uid, ok := store.GetLinkedUser("ABC123")
	if !ok || uid != fakeUserID {
		t.Fatalf("link not persisted: ok=%v uid=%q", ok, uid)
	}
}

func TestStartOnboard_DifferentUserConflict(t *testing.T) {
	store := NewMemoryOnboardStore()
	_ = store.Mint("ABC123")
	_ = store.Link("ABC123", "other-user")
	api := newAPI(t, &clients.Set{}).WithOnboardStore(store)
	r := withAuth(startReq(t, "ABC123"))
	w := httptest.NewRecorder()
	api.StartOnboard(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCompleteOnboard_NotLinkedYet412(t *testing.T) {
	store := NewMemoryOnboardStore()
	_ = store.Mint("ABC123")
	api := newAPI(t, &clients.Set{}).WithOnboardStore(store)
	body, _ := json.Marshal(map[string]any{
		"token": "ABC123",
		"defaults": OnboardingDefaults{
			BandwidthCapGB: 50, CPUCapPct: 30, IdleOnly: true, PayoutTier: "defer",
		},
	})
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/onboard/complete", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.CompleteOnboard(w, r)
	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("want 412, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCompleteOnboard_BadDefaults(t *testing.T) {
	store := NewMemoryOnboardStore()
	_ = store.Mint("ABC123")
	_ = store.Link("ABC123", fakeUserID)
	api := newAPI(t, &clients.Set{}).WithOnboardStore(store)
	body, _ := json.Marshal(map[string]any{
		"token": "ABC123",
		"defaults": OnboardingDefaults{
			BandwidthCapGB: -1, CPUCapPct: 30, PayoutTier: "defer",
		},
	})
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/onboard/complete", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.CompleteOnboard(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 on bad defaults, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCompleteOnboard_HappyPathBurnsCode(t *testing.T) {
	store := NewMemoryOnboardStore()
	_ = store.Mint("ABC123")
	_ = store.Link("ABC123", fakeUserID)
	api := newAPI(t, &clients.Set{}).WithOnboardStore(store)
	body, _ := json.Marshal(map[string]any{
		"token": "ABC123",
		"defaults": OnboardingDefaults{
			BandwidthCapGB: 50, CPUCapPct: 30, IdleOnly: true,
			Categories: []string{"ecommerce", "seo"}, PayoutTier: "defer",
		},
	})
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/onboard/complete", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.CompleteOnboard(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if _, ok := store.GetLinkedUser("ABC123"); ok {
		t.Fatal("code should have been burned after complete")
	}
}

func TestPollOnboard_Stages(t *testing.T) {
	store := NewMemoryOnboardStore()
	api := newAPI(t, &clients.Set{}).WithOnboardStore(store)

	// awaiting_signin
	body, _ := json.Marshal(map[string]string{"token": "ABC123", "daemon_pubkey": "k"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/onboard/poll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	api.PollOnboard(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202 awaiting_signin, got %d body=%s", w.Code, w.Body.String())
	}

	// awaiting_wizard
	_ = store.Mint("ABC123")
	_ = store.Link("ABC123", "user-1")
	r = httptest.NewRequest(http.MethodPost, "/api/v1/onboard/poll", bytes.NewReader(body))
	w = httptest.NewRecorder()
	api.PollOnboard(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202 awaiting_wizard, got %d body=%s", w.Code, w.Body.String())
	}

	// paired
	_ = store.StoreDefaults("ABC123", OnboardingDefaults{BandwidthCapGB: 50, PayoutTier: "defer"})
	r = httptest.NewRequest(http.MethodPost, "/api/v1/onboard/poll", bytes.NewReader(body))
	w = httptest.NewRecorder()
	api.PollOnboard(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 paired, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPairingCodeFormat(t *testing.T) {
	good := []string{"ABC123", "ZZZZZZ", "0123456789ABCDEFGHJKMNPQRSTVWXYZ"[:6]}
	for _, g := range good {
		if !PairingCodeFormat.MatchString(g) {
			t.Errorf("expected %q to match", g)
		}
	}
	bad := []string{"abc123", "ABCDE", "1234567", "AB-123", "IIIIII", "OOOOOO", "UUUUUU"}
	for _, b := range bad {
		if PairingCodeFormat.MatchString(b) {
			t.Errorf("expected %q NOT to match", b)
		}
	}
}
