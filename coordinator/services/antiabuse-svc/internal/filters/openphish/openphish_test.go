package openphish

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleFeed = `https://malicious.example.com/login
http://evil.test/phish
https://MIXED.case.test/
`

func TestRefreshAndLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()

	b := New(Options{FeedURL: srv.URL})
	if err := b.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh err: %v", err)
	}
	if b.Size() != 3 {
		t.Errorf("Size = %d, want 3", b.Size())
	}
	r := b.CheckURL(context.Background(), "http://evil.test/phish")
	if !r.Match {
		t.Errorf("expected match for known phish; got %+v", r)
	}
	r = b.CheckURL(context.Background(), "https://mixed.case.test/")
	if !r.Match {
		t.Errorf("expected case-normalised match; got %+v", r)
	}
	r = b.CheckURL(context.Background(), "https://example.com/")
	if r.Match {
		t.Errorf("expected no match for benign; got %+v", r)
	}
}

func TestRefresh_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	b := New(Options{FeedURL: srv.URL})
	if err := b.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh should fail on 503")
	}
}

func TestCheckDomain_Noop(t *testing.T) {
	b := New(Options{FeedURL: "http://unused"})
	if r := b.CheckDomain(context.Background(), "any"); r.Match {
		t.Errorf("CheckDomain should not match")
	}
}
