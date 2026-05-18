package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_AllowedOriginEchoed(t *testing.T) {
	mw := Middleware(Options{AllowedOrigins: []string{"https://app.iogrid.org"}})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "https://app.iogrid.org")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.iogrid.org" {
		t.Fatalf("allow-origin: %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials: %q", got)
	}
}

func TestCORS_DisallowedOriginNoHeader(t *testing.T) {
	mw := Middleware(Options{AllowedOrigins: []string{"https://app.iogrid.org"}})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allow-origin: %q", got)
	}
}

func TestCORS_PreflightShortCircuits(t *testing.T) {
	mw := Middleware(Options{AllowedOrigins: []string{"https://app.iogrid.org"}})
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	r := httptest.NewRequest("OPTIONS", "/", nil)
	r.Header.Set("Origin", "https://app.iogrid.org")
	r.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight should 204, got %d", w.Code)
	}
	if called {
		t.Fatal("preflight should not invoke downstream handler")
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("missing allow-methods")
	}
}
