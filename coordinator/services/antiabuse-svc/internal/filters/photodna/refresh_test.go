package photodna

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRefresher_StubMode_NoOp(t *testing.T) {
	b := New(Options{}) // no key → stub
	r := NewRefresher(b, RefresherOptions{})
	// Start must not panic and must return immediately.
	r.Start(context.Background())
	if b.HasBloom() {
		t.Errorf("stub-mode Refresher must not install a bloom filter")
	}
	if err := r.Refresh(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Refresh in stub mode = %v, want ErrNotConfigured", err)
	}
}

func TestRefresher_Refresh_BuildsBloom(t *testing.T) {
	hashList := strings.Join([]string{
		"# NCMEC PhotoDNA hash export — placeholder format",
		"abcdef1234",
		"deadbeefcafe",
		"0011223344",
		"", // blank line — must be skipped
		"5566778899",
	}, "\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(hashList))
	}))
	defer srv.Close()

	b := New(Options{APIKey: "test-key", BaseURL: srv.URL})
	r := NewRefresher(b, RefresherOptions{
		ExpectedHashes:    100,
		FalsePositiveRate: 0.01,
	})
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !b.HasBloom() {
		t.Fatal("bloom not installed after Refresh")
	}
	_, n, err := r.Status()
	if err != nil {
		t.Errorf("Status returns error after success: %v", err)
	}
	if n != 4 {
		t.Errorf("hash count = %d, want 4", n)
	}
}

func TestRefresher_Refresh_HTTPFailure_RecordsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	b := New(Options{APIKey: "test-key", BaseURL: srv.URL})
	r := NewRefresher(b, RefresherOptions{})
	if err := r.Refresh(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
	_, _, last := r.Status()
	if last == nil {
		t.Errorf("Status.lastErr should be populated")
	}
}

func TestRefresher_LoopExitsOnCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("aaaa\nbbbb\n"))
	}))
	defer srv.Close()
	b := New(Options{APIKey: "test-key", BaseURL: srv.URL})
	r := NewRefresher(b, RefresherOptions{Interval: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	// Wait up to 2s for the initial refresh to install the bloom —
	// the goroutine schedule under -race + CI load can be slow.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.HasBloom() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if !b.HasBloom() {
		t.Errorf("bloom should be installed after initial Refresh")
	}
}

func TestRefresher_DefaultsApplied(t *testing.T) {
	b := New(Options{APIKey: "k"})
	r := NewRefresher(b, RefresherOptions{}) // all zeros
	if r.opts.Interval != DefaultRefreshInterval {
		t.Errorf("Interval default not applied: %v", r.opts.Interval)
	}
	if r.opts.ExportPath != DefaultHashExportPath {
		t.Errorf("ExportPath default not applied: %v", r.opts.ExportPath)
	}
	if r.opts.ExpectedHashes != 1_000_000 {
		t.Errorf("ExpectedHashes default not applied: %v", r.opts.ExpectedHashes)
	}
}
