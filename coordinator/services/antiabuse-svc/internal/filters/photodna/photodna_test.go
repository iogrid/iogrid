package photodna

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
)

func TestStubMode_FailsClosedToReview(t *testing.T) {
	b := New(Options{}) // no APIKey → stub; AllowUnscanned defaults false
	if b.Enabled() {
		t.Error("backend should not be Enabled() without APIKey")
	}
	r := b.CheckURL(context.Background(), "https://example.com/img.jpg")
	// Fail CLOSED: an unconfigured CSAM backend must NOT silently ALLOW.
	if r.Decision == antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("stub mode (AllowUnscanned=false) must NOT ALLOW (fail-closed): %+v", r)
	}
	if r.Decision != antiabusev1.FilterDecision_FILTER_DECISION_REVIEW {
		t.Errorf("Decision = %v, want REVIEW", r.Decision)
	}
	if r.Reason != "csam_backend_unconfigured" {
		t.Errorf("Reason = %q, want csam_backend_unconfigured", r.Reason)
	}
	if r.Match {
		t.Errorf("REVIEW result should not set Match=true: %+v", r)
	}
	// Second call must not double-log; warned flag is sticky.
	_ = b.CheckURL(context.Background(), "https://example.com/img2.jpg")
}

func TestStubMode_AllowUnscanned_Allows(t *testing.T) {
	b := New(Options{AllowUnscanned: true}) // explicit dev/test opt-out
	if b.Enabled() {
		t.Error("backend should not be Enabled() without APIKey")
	}
	r := b.CheckURL(context.Background(), "https://example.com/img.jpg")
	if r.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("AllowUnscanned=true must ALLOW (explicit opt-out): %+v", r)
	}
	if r.Match {
		t.Errorf("ALLOW result must not match: %+v", r)
	}
}

func TestStubMode_SyntheticCSAMFixture_BlocksBothModes(t *testing.T) {
	const csamURL = "https://host.example/csam-test-fixture/x.jpg"
	for _, allowUnscanned := range []bool{false, true} {
		b := New(Options{AllowUnscanned: allowUnscanned})
		r := b.CheckURL(context.Background(), csamURL)
		if r.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
			t.Errorf("AllowUnscanned=%v: synthetic CSAM fixture must BLOCK; got %+v", allowUnscanned, r)
		}
		if r.Reason != "csam_hash_match" {
			t.Errorf("AllowUnscanned=%v: Reason = %q, want csam_hash_match", allowUnscanned, r.Reason)
		}
	}
}

func TestEnabled_WithKey(t *testing.T) {
	b := New(Options{APIKey: "test-key"})
	if !b.Enabled() {
		t.Error("backend should be Enabled() with APIKey")
	}
}

func TestInjectMatch_ProducesBlock(t *testing.T) {
	// Use a server that always returns no-match so the injection path
	// is what produces the BLOCK (not the network result).
	srv := newMatchServer(t, false, "")
	defer srv.Close()

	b := New(Options{APIKey: "test-key", BaseURL: srv.URL})
	url := "https://example.com/csam.jpg"
	hash := hashOfURL(url)
	b.InjectMatch(hash)
	r := b.CheckURL(context.Background(), url)
	if !r.Match {
		t.Fatalf("expected match after InjectMatch: %+v", r)
	}
	if r.Reason != "csam_hash_match" {
		t.Errorf("Reason = %q, want csam_hash_match", r.Reason)
	}
}

func TestCheckDomain_AlwaysAllow(t *testing.T) {
	b := New(Options{APIKey: "test-key"})
	if r := b.CheckDomain(context.Background(), "anything"); r.Match {
		t.Errorf("CheckDomain must not match (PhotoDNA is per-image)")
	}
}

func TestCheckURL_HTTPMatch_Blocks(t *testing.T) {
	srv := newMatchServer(t, true, "csam_a1")
	defer srv.Close()
	b := New(Options{APIKey: "test-key", BaseURL: srv.URL})
	r := b.CheckURL(context.Background(), "https://malicious.test/x.jpg")
	if !r.Match || r.Decision == 0 {
		t.Fatalf("expected BLOCK from HTTP match, got %+v", r)
	}
	if !strings.Contains(r.Explanation, "csam_a1") {
		t.Errorf("Explanation = %q, want category to surface", r.Explanation)
	}
	checks, matches, _ := b.Stats()
	if checks != 1 || matches != 1 {
		t.Errorf("Stats() = (%d,%d,_), want (1,1,_)", checks, matches)
	}
}

func TestCheckURL_HTTPNoMatch_Allows(t *testing.T) {
	srv := newMatchServer(t, false, "")
	defer srv.Close()
	b := New(Options{APIKey: "test-key", BaseURL: srv.URL})
	r := b.CheckURL(context.Background(), "https://benign.test/x.jpg")
	if r.Match {
		t.Fatalf("expected ALLOW from no-match HTTP, got %+v", r)
	}
}

func TestCheckURL_HTTPError_TreatsAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	b := New(Options{APIKey: "test-key", BaseURL: srv.URL})
	r := b.CheckURL(context.Background(), "https://err.test/x.jpg")
	if r.Match {
		t.Fatalf("error result must not BLOCK (best-effort): %+v", r)
	}
	if r.Err == nil {
		t.Errorf("expected non-nil Err on 5xx")
	}
	_, _, errs := b.Stats()
	if errs == 0 {
		t.Errorf("expected error counter to increment")
	}
}

func TestCheckURL_AuthHeaderForwarded(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(hashLookupResponse{Match: false})
	}))
	defer srv.Close()
	b := New(Options{APIKey: "secret-key", BaseURL: srv.URL})
	_ = b.CheckURL(context.Background(), "https://x.test/img.jpg")
	if seenAuth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want Bearer secret-key", seenAuth)
	}
}

func TestBloom_ShortCircuitsNonMembers(t *testing.T) {
	// Server that would BLOCK if asked — but bloom says "definitely
	// not", so we expect ALLOW with no API call.
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(hashLookupResponse{Match: true, MatchCategory: "should_not_see"})
	}))
	defer srv.Close()
	b := New(Options{APIKey: "test-key", BaseURL: srv.URL})

	// Empty bloom — nothing is a member.
	bf := NewBloom(1000, 0.001)
	b.SetBloom(bf)

	r := b.CheckURL(context.Background(), "https://x.test/img.jpg")
	if r.Match {
		t.Fatalf("bloom non-member must ALLOW: %+v", r)
	}
	if called {
		t.Errorf("API must NOT be called when bloom rules out the hash")
	}
}

func TestBloom_HitFallsThroughToAPI(t *testing.T) {
	srv := newMatchServer(t, true, "csam_b2")
	defer srv.Close()
	b := New(Options{APIKey: "test-key", BaseURL: srv.URL})

	url := "https://csam.test/img.jpg"
	bf := NewBloom(1000, 0.001)
	bf.Add(hashOfURL(url))
	b.SetBloom(bf)

	r := b.CheckURL(context.Background(), url)
	if !r.Match {
		t.Fatalf("bloom-positive should fall through to API match, got %+v", r)
	}
}

func TestStubMode_WarnOnlyOnce(t *testing.T) {
	b := New(Options{})
	if b.warned.Load() {
		t.Fatal("warned flag should start unset")
	}
	_ = b.CheckURL(context.Background(), "https://x/1.jpg")
	if !b.warned.Load() {
		t.Fatal("warned flag should be set after first call")
	}
	// Subsequent calls are idempotent (covered by the atomic Swap path).
	_ = b.CheckURL(context.Background(), "https://x/2.jpg")
}

// newMatchServer returns an httptest.Server that responds to the
// match endpoint with a deterministic outcome. Used by every HTTP test.
func newMatchServer(t *testing.T, match bool, category string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, matchEndpoint) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			http.Error(w, "bad content-type", http.StatusUnsupportedMediaType)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hashLookupResponse{Match: match, MatchCategory: category})
	}))
}

func TestCheckURL_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(hashLookupResponse{Match: false})
	}))
	defer srv.Close()

	b := New(Options{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: &http.Client{Timeout: 10 * time.Millisecond},
	})
	r := b.CheckURL(context.Background(), "https://slow.test/img.jpg")
	if r.Err == nil {
		t.Errorf("expected timeout error, got %+v", r)
	}
}
