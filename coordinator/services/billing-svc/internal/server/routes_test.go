package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestIndexHandler exercises the stub fallback so the test harness can
// hit /v1 without any deps wired.
func TestIndexHandler(t *testing.T) {
	r := chi.NewRouter()
	MountStub(r)
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v1/")
	if err != nil {
		t.Fatalf("GET /v1/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := make([]byte, 256)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "billing-svc") {
		t.Errorf("body missing service name: %q", string(body[:n]))
	}
}

func TestSanitizeTier(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"SUBSCRIPTION_TIER_PAYG", "PAYG"},
		{"subscription_tier_growth", "GROWTH"},
		{"PAYG", "PAYG"},
	}
	for _, c := range cases {
		if got := SanitizeTier(c.in); got != c.want {
			t.Errorf("SanitizeTier(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStatusFromTier(t *testing.T) {
	for _, tier := range []string{"PAYG", "STARTER", "GROWTH", "ENTERPRISE"} {
		if statusFromTier[tier] != "active" {
			t.Errorf("expected active for %s", tier)
		}
	}
}
