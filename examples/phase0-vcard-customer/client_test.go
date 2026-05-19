package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestExtractProfile_CanonicalSSR exercises the og:title + og:description
// path against the fixture HTML the production page ships in 2026-05.
func TestExtractProfile_CanonicalSSR(t *testing.T) {
	r := ExtractProfile(fixtureHTML)
	if r.Name != "Satya Nadella" {
		t.Fatalf("name: got %q want %q", r.Name, "Satya Nadella")
	}
	if r.Title != "Chairman and CEO at Microsoft" {
		t.Fatalf("title: got %q", r.Title)
	}
	if r.Company != "Microsoft" {
		t.Fatalf("company: got %q", r.Company)
	}
}

// TestExtractProfile_TwoSegmentTitle covers the "Name - Title at Company"
// SSR variant LinkedIn occasionally falls back to (no explicit company
// segment in og:title). The extractor derives company by splitting on
// " at " in the title.
func TestExtractProfile_TwoSegmentTitle(t *testing.T) {
	html := `<!DOCTYPE html><html><head>
<meta property="og:title" content="Jane Doe - VP Engineering at Acme Robotics | LinkedIn">
</head><body></body></html>`
	r := ExtractProfile(html)
	if r.Name != "Jane Doe" {
		t.Fatalf("name: got %q", r.Name)
	}
	if r.Title != "VP Engineering at Acme Robotics" {
		t.Fatalf("title: got %q", r.Title)
	}
	if r.Company != "Acme Robotics" {
		t.Fatalf("company: got %q", r.Company)
	}
}

// TestExtractProfile_NoMetaFallbackToTitle covers the case where the
// rendered page omits og:* tags. The <title> element is still authoritative
// for the name. Title/company stay empty — the caller should retry or fall
// back to a richer extractor.
func TestExtractProfile_NoMetaFallbackToTitle(t *testing.T) {
	html := `<!DOCTYPE html><html><head>
<title>Tim Cook | LinkedIn</title>
</head><body></body></html>`
	r := ExtractProfile(html)
	if r.Name != "Tim Cook" {
		t.Fatalf("name: got %q", r.Name)
	}
	if r.Title != "" || r.Company != "" {
		t.Fatalf("expected empty title/company on title-only fallback, got %+v", r)
	}
}

// TestExtractProfile_NoSignal handles a totally-broken page (e.g.
// LinkedIn's "Sign in to continue" stub). Extractor must not panic and
// must return an empty result.
func TestExtractProfile_NoSignal(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title></title></head><body><p>Sign in</p></body></html>`
	r := ExtractProfile(html)
	if r.Name != "" || r.Title != "" || r.Company != "" {
		t.Fatalf("expected empty result on signal-less page, got %+v", r)
	}
}

// TestSplitMetaTitle_StripsLinkedInSuffix asserts the " | LinkedIn"
// suffix is dropped before the dash-split. (Without this we'd surface
// "Microsoft | LinkedIn" as the company.)
func TestSplitMetaTitle_StripsLinkedInSuffix(t *testing.T) {
	parts := splitMetaTitle("A - B - C | LinkedIn")
	if len(parts) != 3 || parts[2] != "C" {
		t.Fatalf("got %#v, want [A B C]", parts)
	}
}

// TestEnrichVanity_DryRun verifies the -dry-run code path returns a
// deterministic result without making any network calls. This is what
// CI uses to exercise the binary without actually scraping LinkedIn.
func TestEnrichVanity_DryRun(t *testing.T) {
	cfg := &Config{Vanity: "satyanadella", DryRun: true}
	r, err := EnrichVanity(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("dry-run enrich: %v", err)
	}
	if r.Name != "Satya Nadella" {
		t.Fatalf("name: %q", r.Name)
	}
	if r.ProxyUsed {
		t.Fatalf("dry-run must not claim proxy_used=true")
	}
}

// TestLoadConfig_RequiresAPIKey asserts that wet-run invocations fail
// loudly without IOGRID_API_KEY / IOGRID_WORKSPACE — we don't want a
// silent fall-through that hits LinkedIn directly from the caller's IP.
func TestLoadConfig_RequiresAPIKey(t *testing.T) {
	t.Setenv("IOGRID_API_KEY", "")
	t.Setenv("IOGRID_WORKSPACE", "")
	_, err := loadConfig([]string{"-vanity", "satyanadella"})
	if err == nil {
		t.Fatal("expected loadConfig to refuse wet-run without IOGRID_API_KEY")
	}
	if !strings.Contains(err.Error(), "IOGRID_API_KEY") {
		t.Fatalf("error should mention IOGRID_API_KEY, got: %v", err)
	}
}

// TestLoadConfig_DryRunSkipsCredentialCheck makes sure CI can run the
// dry-run path with no env set (it's the whole point of -dry-run).
func TestLoadConfig_DryRunSkipsCredentialCheck(t *testing.T) {
	t.Setenv("IOGRID_API_KEY", "")
	t.Setenv("IOGRID_WORKSPACE", "")
	cfg, err := loadConfig([]string{"-dry-run"})
	if err != nil {
		t.Fatalf("dry-run config should not require creds: %v", err)
	}
	if !cfg.DryRun {
		t.Fatal("DryRun flag not set")
	}
}

// TestEnrichVanity_HTTPRoundTrip exercises the real client.Do path
// against an in-process mock server pretending to be LinkedIn. This
// verifies that header injection, body cap, and provider-country
// surfacing work — no SOCKS5 here (covered in the e2e smoke).
func TestEnrichVanity_HTTPRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/in/satyanadella") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing UA")
		}
		w.Header().Set("X-Iogrid-Provider-Country", "US")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(fixtureHTML))
	}))
	defer srv.Close()

	cfg := &Config{Vanity: "satyanadella", UserAgent: "test/1.0"}
	// Override target via a custom client that rewrites the request URL.
	hc := &http.Client{
		Transport: rewriteTransport{base: http.DefaultTransport, rewriteTo: srv.URL},
	}
	r, err := EnrichVanity(context.Background(), cfg, hc)
	if err != nil {
		t.Fatalf("enrich: %v", err)
	}
	if r.Name != "Satya Nadella" {
		t.Fatalf("name: %q", r.Name)
	}
	if !r.ProxyUsed {
		t.Fatal("wet-run path must set proxy_used=true")
	}
	if r.ProviderCountry != "US" {
		t.Fatalf("provider_country: %q", r.ProviderCountry)
	}
}

// rewriteTransport routes every request to a fixed base URL, preserving
// path/query. Used only by tests so we don't need to hit real LinkedIn.
type rewriteTransport struct {
	base      http.RoundTripper
	rewriteTo string
}

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Build a new URL = rewriteTo + original path.
	r2 := r.Clone(r.Context())
	u := r2.URL
	// Strip scheme/host, prepend the test server's URL.
	target := rt.rewriteTo + u.RequestURI()
	req2, err := http.NewRequestWithContext(r2.Context(), r2.Method, target, r2.Body)
	if err != nil {
		return nil, err
	}
	req2.Header = r2.Header
	return rt.base.RoundTrip(req2)
}
