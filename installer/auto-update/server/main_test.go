package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleManifestServesBody(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	body := []byte(`{"version":1,"channel":"stable","releases":[],"signature":{"algorithm":"ed25519","key_id":"k","value":""}}`)
	if err := os.WriteFile(manifest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	s := newServer(manifest, dir, 30)
	if err := s.preload(); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.handleManifest(rr, httptest.NewRequest(http.MethodGet, "/manifest.json", nil))
	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	if rr.Body.String() != string(body) {
		t.Fatalf("body mismatch: %s", rr.Body.String())
	}
	if rr.Header().Get("ETag") == "" {
		t.Fatalf("etag header missing")
	}
}

func TestHandleManifestReturns304OnETagMatch(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	body := []byte(`{}`)
	if err := os.WriteFile(manifest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	s := newServer(manifest, dir, 30)
	if err := s.preload(); err != nil {
		t.Fatal(err)
	}
	rr1 := httptest.NewRecorder()
	s.handleManifest(rr1, httptest.NewRequest(http.MethodGet, "/manifest.json", nil))
	etag := rr1.Header().Get("ETag")
	req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	req.Header.Set("If-None-Match", etag)
	rr2 := httptest.NewRecorder()
	s.handleManifest(rr2, req)
	if rr2.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", rr2.Code)
	}
}

func TestHandleBinaryServesFile(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "0.1.0")
	if err := os.MkdirAll(release, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("\x7fELFfakedaemonbinary")
	if err := os.WriteFile(filepath.Join(release, "iogridd-linux-amd64"), content, 0o755); err != nil {
		t.Fatal(err)
	}
	s := newServer(filepath.Join(dir, "manifest.json"), dir, 30)
	rr := httptest.NewRecorder()
	s.handleBinary(rr, httptest.NewRequest(http.MethodGet, "/0.1.0/iogridd-linux-amd64", nil))
	if rr.Code != 200 {
		t.Fatalf("status = %d body=%q", rr.Code, rr.Body.String())
	}
	got, _ := io.ReadAll(rr.Body)
	if string(got) != string(content) {
		t.Fatalf("body mismatch")
	}
}

func TestHandleBinaryRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	s := newServer(filepath.Join(dir, "manifest.json"), dir, 30)
	cases := []string{
		"/../etc/passwd",
		"/..",
		"//etc",
		"/0.1.0/../../etc/passwd",
		"/0.1.0/bin/extra",
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		s.handleBinary(rr, httptest.NewRequest(http.MethodGet, c, nil))
		if rr.Code == 200 {
			t.Fatalf("traversal accepted: %s status=%d", c, rr.Code)
		}
	}
}

func TestValidReleasePath(t *testing.T) {
	good := []string{"0.1.0/iogridd", "v1/x"}
	for _, p := range good {
		if !validReleasePath(p) {
			t.Fatalf("expected valid: %q", p)
		}
	}
	bad := []string{"", "/", "0.1.0", "../etc", "0.1.0/..", "0.1.0\\evil", "a/b/c"}
	for _, p := range bad {
		if validReleasePath(p) {
			t.Fatalf("expected invalid: %q", p)
		}
	}
}

func TestHealthz(t *testing.T) {
	s := newServer("/dev/null", "/tmp", 30)
	rr := httptest.NewRecorder()
	s.handleHealth(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
}
