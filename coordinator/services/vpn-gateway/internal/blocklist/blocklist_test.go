package blocklist

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const stevenBlackSample = `# Title: StevenBlack/hosts (sample)
# This is a comment.

0.0.0.0 doubleclick.net
0.0.0.0 google-analytics.com
0.0.0.0 ads.example.com
0.0.0.0 evil-tracker.net  # inline comment
0.0.0.0 localhost
0.0.0.0 broadcasthost
# Bad line below — should be skipped
0.0.0.0
not-a-host
0.0.0.0 sub.sub.deep.example.org
`

func TestLoadAndBlock(t *testing.T) {
	s := New()
	n, err := s.Load(strings.NewReader(stevenBlackSample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if n < 4 {
		t.Errorf("loaded %d hosts, expected at least 4", n)
	}

	// Exact matches
	for _, h := range []string{
		"doubleclick.net",
		"google-analytics.com",
		"ads.example.com",
		"evil-tracker.net",
		"sub.sub.deep.example.org",
	} {
		if !s.Block(h) {
			t.Errorf("Block(%q) = false, want true (exact match)", h)
		}
	}

	// Subdomain matches
	for _, h := range []string{
		"www.doubleclick.net",
		"a.b.c.doubleclick.net",
		"track.google-analytics.com",
	} {
		if !s.Block(h) {
			t.Errorf("Block(%q) = false, want true (subdomain)", h)
		}
	}

	// Misses
	for _, h := range []string{
		"example.com", // covered domain ads.example.com is more-specific only
		"openova.io",
		"github.com",
		"doubleclick.org", // different TLD
		"",
	} {
		if s.Block(h) {
			t.Errorf("Block(%q) = true, want false", h)
		}
	}

	// Skipped lines
	if s.Block("localhost") {
		t.Error("localhost should be skipped from list")
	}
	if s.Block("broadcasthost") {
		t.Error("broadcasthost should be skipped from list")
	}
}

func TestBlockCaseAndTrailingDot(t *testing.T) {
	s := New()
	_, err := s.Load(strings.NewReader("0.0.0.0 Tracker.Example.Com\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Block("tracker.example.com") {
		t.Error("case-insensitive exact match should hit")
	}
	if !s.Block("TRACKER.EXAMPLE.COM") {
		t.Error("uppercase query should hit")
	}
	if !s.Block("tracker.example.com.") {
		t.Error("trailing dot should be stripped")
	}
	if !s.Block("a.tracker.example.com.") {
		t.Error("trailing dot + subdomain should hit")
	}
}

func TestCollapsing(t *testing.T) {
	// If the shorter suffix is inserted first, the more-specific insertion
	// is a no-op; size still 1.
	s := New()
	n, err := s.Load(strings.NewReader(
		"0.0.0.0 example.com\n" +
			"0.0.0.0 ads.example.com\n",
	))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 unique entry (example.com swallows ads.example.com), got %d", n)
	}
	if !s.Block("anything.example.com") {
		t.Error("should match example.com suffix")
	}

	// If the more-specific is inserted first, then the broader suffix
	// is added — both will be blocked but the broader supersedes.
	s2 := New()
	_, _ = s2.Load(strings.NewReader(
		"0.0.0.0 ads.example.com\n" +
			"0.0.0.0 example.com\n",
	))
	if !s2.Block("foo.example.com") {
		t.Error("broader insert should still match")
	}
}

func TestLoadURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0.0.0.0 fetched.example.com\n"))
	}))
	defer srv.Close()
	s := New()
	n, err := s.LoadURL(nil, srv.URL)
	if err != nil {
		t.Fatalf("LoadURL: %v", err)
	}
	if n != 1 {
		t.Errorf("LoadURL count = %d, want 1", n)
	}
	if !s.Block("fetched.example.com") {
		t.Error("fetched host should be blocked")
	}
}

func TestSizeAfterReload(t *testing.T) {
	s := New()
	_, _ = s.Load(strings.NewReader("0.0.0.0 a.com\n0.0.0.0 b.com\n"))
	if s.Size() != 2 {
		t.Errorf("size after first load = %d, want 2", s.Size())
	}
	_, _ = s.Load(strings.NewReader("0.0.0.0 c.com\n"))
	if s.Size() != 1 {
		t.Errorf("size after reload = %d, want 1 (atomic swap)", s.Size())
	}
	if s.Block("a.com") {
		t.Error("a.com should be gone after reload")
	}
}

func TestHostnameOnlyFormat(t *testing.T) {
	s := New()
	_, err := s.Load(strings.NewReader("ads.example.com\ntracker.net\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Block("ads.example.com") {
		t.Error("hostname-only format should parse")
	}
}
