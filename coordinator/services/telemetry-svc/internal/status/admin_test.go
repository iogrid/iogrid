package status

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/incidents"
)

func TestAdminAuth_DisabledWithoutToken(t *testing.T) {
	mux := chi.NewRouter()
	mux.With(AdminAuth("")).Post("/x", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503 (admin disabled)", rec.Code)
	}
}

func TestAdminAuth_RejectsMissingAndWrongToken(t *testing.T) {
	mux := chi.NewRouter()
	mux.With(AdminAuth("s3cret")).Post("/x", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name string
		hdr  string
		code int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic foo", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong", http.StatusUnauthorized},
		{"right token", "Bearer s3cret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/x", nil)
			if tc.hdr != "" {
				req.Header.Set("Authorization", tc.hdr)
			}
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.code {
				t.Errorf("code = %d, want %d", rec.Code, tc.code)
			}
		})
	}
}

func TestCreateIncidentHandler_HappyPath(t *testing.T) {
	store := incidents.NewInMemory()
	body, _ := json.Marshal(incidents.CreateIncidentInput{
		Title: "Proxy outage", Impact: incidents.ImpactCritical,
		AffectedServices: []string{"proxy-gateway"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/status/incidents", bytes.NewReader(body))
	CreateIncidentHandler(store)(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	active, _ := store.ListActive(req.Context())
	if len(active) != 1 {
		t.Errorf("active = %d, want 1", len(active))
	}
}

func TestCreateIncidentHandler_RejectsBadJSON(t *testing.T) {
	store := incidents.NewInMemory()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/status/incidents", bytes.NewReader([]byte("not json")))
	CreateIncidentHandler(store)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rec.Code)
	}
}

func TestSubscribeHandler_RateLimited(t *testing.T) {
	store := incidents.NewInMemory()
	h := SubscribeHandler(store)
	// First request from a fresh IP — should succeed.
	body, _ := json.Marshal(incidents.SubscribeInput{Email: "x@example.com"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/status/subscribe", bytes.NewReader(body))
	req.RemoteAddr = "9.9.9.9:1234"
	h(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first request code = %d, body=%s", rec.Code, rec.Body.String())
	}
	// Burn down the bucket from the same IP.
	for i := 0; i < 70; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/status/subscribe", bytes.NewReader(body))
		req.RemoteAddr = "9.9.9.9:1234"
		h(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			return // rate limit fired — pass.
		}
	}
	t.Errorf("rate limiter never fired after 70 requests from same IP")
}
