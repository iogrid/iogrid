package gsb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
)

func newMockServer(t *testing.T, body string, status int) (*httptest.Server, *int) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Errorf("expected POST; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return srv, &calls
}

func TestCheckURL_Match(t *testing.T) {
	resp := `{"matches":[{"threatType":"SOCIAL_ENGINEERING"}]}`
	srv, _ := newMockServer(t, resp, 200)
	defer srv.Close()
	b := New(Options{APIKey: "k", Endpoint: srv.URL})
	r := b.CheckURL(context.Background(), "https://evil.example/")
	if !r.Match {
		t.Fatalf("expected match: %+v", r)
	}
	if r.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Errorf("Decision = %v, want BLOCK", r.Decision)
	}
}

func TestCheckURL_NoMatch(t *testing.T) {
	srv, _ := newMockServer(t, `{}`, 200)
	defer srv.Close()
	b := New(Options{APIKey: "k", Endpoint: srv.URL})
	r := b.CheckURL(context.Background(), "https://safe.example/")
	if r.Match {
		t.Errorf("expected no match: %+v", r)
	}
}

func TestDisabled_NoNetworkCall(t *testing.T) {
	srv, calls := newMockServer(t, `{}`, 200)
	defer srv.Close()
	b := New(Options{APIKey: "", Endpoint: srv.URL})
	r := b.CheckURL(context.Background(), "https://anything/")
	if r.Match {
		t.Errorf("disabled backend should not match")
	}
	if *calls != 0 {
		t.Errorf("disabled backend made %d HTTP calls; want 0", *calls)
	}
}

func TestCache_HitsAreReused(t *testing.T) {
	srv, calls := newMockServer(t, `{}`, 200)
	defer srv.Close()
	b := New(Options{APIKey: "k", Endpoint: srv.URL})
	for i := 0; i < 5; i++ {
		_ = b.CheckURL(context.Background(), "https://repeat.example/")
	}
	if *calls != 1 {
		t.Errorf("expected 1 HTTP call (cached); got %d", *calls)
	}
}

func TestCheckURL_HTTPError(t *testing.T) {
	srv, _ := newMockServer(t, `nope`, 500)
	defer srv.Close()
	b := New(Options{APIKey: "k", Endpoint: srv.URL})
	r := b.CheckURL(context.Background(), "https://oops.example/")
	if r.Err == nil {
		t.Errorf("expected Err on 500; got %+v", r)
	}
	if r.Match {
		t.Errorf("on error, must default to no-match")
	}
}

func TestRequestEnvelopeShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	b := New(Options{APIKey: "k", Endpoint: srv.URL})
	_ = b.CheckURL(context.Background(), "https://probe.example/")
	if _, ok := captured["client"]; !ok {
		t.Errorf("missing client envelope: %v", captured)
	}
	if _, ok := captured["threatInfo"]; !ok {
		t.Errorf("missing threatInfo envelope: %v", captured)
	}
}
