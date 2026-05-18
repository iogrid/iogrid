package phishtank

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleFeed = `[
  {"url": "https://malicious.example.com/login"},
  {"url": "http://evil.test/phish"},
  {"url": "HTTPS://Mixed.Case.Example/"}
]`

func TestRefreshAndLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()

	b := New(Options{FeedURL: srv.URL, Refresh: time.Hour})
	if err := b.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh err: %v", err)
	}
	if b.Size() != 3 {
		t.Errorf("Size = %d, want 3", b.Size())
	}

	// Exact match
	r := b.CheckURL(context.Background(), "https://malicious.example.com/login")
	if !r.Match {
		t.Errorf("expected match for known phish URL; got %+v", r)
	}
	// Trailing slash normalised
	r = b.CheckURL(context.Background(), "https://mixed.case.example/")
	if !r.Match {
		t.Errorf("expected match (case + trailing slash normalised); got %+v", r)
	}
	// Miss
	r = b.CheckURL(context.Background(), "https://benign.example.com/")
	if r.Match {
		t.Errorf("expected no match for benign URL; got %+v", r)
	}
}

func TestRefresh_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	b := New(Options{FeedURL: srv.URL})
	if err := b.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh should fail on 500")
	}
}

func TestCheckDomain_Noop(t *testing.T) {
	b := New(Options{FeedURL: "http://unused"})
	r := b.CheckDomain(context.Background(), "anything")
	if r.Match {
		t.Errorf("CheckDomain should never match (URL-only feed)")
	}
}

func TestDisable_ShortCircuits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()
	b := New(Options{FeedURL: srv.URL})
	_ = b.Refresh(context.Background())
	b.Disable()
	r := b.CheckURL(context.Background(), "https://malicious.example.com/login")
	if r.Match {
		t.Errorf("disabled backend should never match")
	}
}
