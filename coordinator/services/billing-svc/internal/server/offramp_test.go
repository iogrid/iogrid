package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOffRamp_DisabledReturns503 exercises the routes when no OffRamp
// service is wired (the common case before OFFRAMP_PROVIDERS is set).
func TestOffRamp_DisabledReturns503(t *testing.T) {
	h := &handlers{} // all deps nil

	cases := []struct {
		method, url string
	}{
		{"POST", "/offramp/start"},
		{"GET", "/offramp/status/abc"},
		{"POST", "/offramp/webhook/moonpay"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.url, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		switch {
		case strings.HasPrefix(c.url, "/offramp/start"):
			h.startOffRamp(w, req)
		case strings.HasPrefix(c.url, "/offramp/status"):
			h.getOffRampStatus(w, req)
		case strings.HasPrefix(c.url, "/offramp/webhook"):
			h.offRampWebhook(w, req)
		}
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status=%d want 503", c.method, c.url, w.Code)
		}
	}
}

// TestSignatureForProvider verifies the per-provider header dispatch.
func TestSignatureForProvider(t *testing.T) {
	cases := []struct {
		provider string
		headers  map[string]string
		want     string
	}{
		{"moonpay", map[string]string{"Moonpay-Signature-V2": "t=1,s=ab"}, "t=1,s=ab"},
		{"sociable-cash", map[string]string{"Cash-Signature": "deadbeef"}, "deadbeef"},
		{"moonpay", map[string]string{"X-Webhook-Signature": "generic"}, "generic"},
		{"unknown", map[string]string{"X-Webhook-Signature": "fallback"}, "fallback"},
		{"moonpay", map[string]string{}, ""},
	}
	for i, c := range cases {
		h := http.Header{}
		for k, v := range c.headers {
			h.Set(k, v)
		}
		if got := signatureForProvider(c.provider, h); got != c.want {
			t.Errorf("case %d: got=%q want=%q", i, got, c.want)
		}
	}
}

// TestListOffRampProviders_NilReturnsEmpty checks the disabled branch
// returns an empty list (the picker is still rendered with disabled
// options).
func TestListOffRampProviders_NilReturnsEmpty(t *testing.T) {
	h := &handlers{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/offramp/providers", nil)
	h.listOffRampProviders(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"providers":[]`) {
		t.Errorf("body=%q", w.Body.String())
	}
}
