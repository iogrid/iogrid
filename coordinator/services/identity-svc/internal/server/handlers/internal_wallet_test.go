package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInternalAuth(t *testing.T) {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	cases := []struct {
		name, token, header string
		want                int
	}{
		{"empty token disabled", "", "anything", http.StatusServiceUnavailable},
		{"wrong token forbidden", "secret", "nope", http.StatusForbidden},
		{"missing header forbidden", "secret", "", http.StatusForbidden},
		{"correct token ok", "secret", "secret", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/internal/v1/users/x/wallet", nil)
			if c.header != "" {
				r.Header.Set("X-Internal-Token", c.header)
			}
			w := httptest.NewRecorder()
			InternalAuth(c.token, ok)(w, r)
			if w.Code != c.want {
				t.Fatalf("token=%q header=%q: got %d want %d", c.token, c.header, w.Code, c.want)
			}
		})
	}
}

func TestInternalGetUserWallet_NoStore(t *testing.T) {
	h := &AuthHandler{} // Store nil
	w := httptest.NewRecorder()
	h.InternalGetUserWallet(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: got %d want 503", w.Code)
	}
}
