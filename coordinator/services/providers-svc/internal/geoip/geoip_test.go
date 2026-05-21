package geoip

import (
	"errors"
	"testing"
)

// --- ExtractClientIP --------------------------------------------------------

func TestExtractClientIP_XForwardedForWins(t *testing.T) {
	headers := map[string]string{
		"X-Forwarded-For": "203.0.113.7, 10.0.0.5",
		"X-Real-Ip":       "10.0.0.5",
	}
	got := ExtractClientIP(func(k string) string { return headers[k] }, "10.0.0.5:443")
	if got != "203.0.113.7" {
		t.Fatalf("want left-most XFF, got %q", got)
	}
}

func TestExtractClientIP_XRealIPFallback(t *testing.T) {
	headers := map[string]string{"X-Real-Ip": "198.51.100.42"}
	got := ExtractClientIP(func(k string) string { return headers[k] }, "10.0.0.5:443")
	if got != "198.51.100.42" {
		t.Fatalf("want X-Real-Ip fallback, got %q", got)
	}
}

func TestExtractClientIP_RemoteAddrFallback(t *testing.T) {
	got := ExtractClientIP(func(string) string { return "" }, "203.0.113.99:54321")
	if got != "203.0.113.99" {
		t.Fatalf("want RemoteAddr host, got %q", got)
	}
}

func TestExtractClientIP_Empty(t *testing.T) {
	got := ExtractClientIP(func(string) string { return "" }, "")
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestExtractClientIP_NoHeaderGetter(t *testing.T) {
	// nil getter must not panic.
	got := ExtractClientIP(nil, "203.0.113.10:443")
	if got != "203.0.113.10" {
		t.Fatalf("want RemoteAddr host, got %q", got)
	}
}

// TestExtractClientIP_Forwarded covers the RFC 7239 path added in
// #381. We prefer `Forwarded` over `X-Forwarded-For` when both are
// present because it's the standardised header — proxies that emit
// both are guaranteed to keep them in sync, and on the next-gen
// Cilium Gateway path only `Forwarded` is emitted.
func TestExtractClientIP_Forwarded(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		want    string
		xffSet  string // optional X-Forwarded-For, to assert preference
		xffWant string // expected when Forwarded is absent (sanity check)
	}{
		{"single for=ipv4", `for=203.0.113.7`, "203.0.113.7", "", ""},
		{"for= with quotes", `for="203.0.113.7"`, "203.0.113.7", "", ""},
		{"for= with port", `for="203.0.113.7:54321"`, "203.0.113.7", "", ""},
		{"for= with ipv6 brackets", `for="[2001:db8::1]:54321"`, "2001:db8::1", "", ""},
		{"mixed-case directive", `For=203.0.113.7`, "203.0.113.7", "", ""},
		{"with proto + by", `proto=https; for=203.0.113.7; by=10.0.0.5`, "203.0.113.7", "", ""},
		{"multi-element left-most wins", `for=203.0.113.7, for=10.0.0.5`, "203.0.113.7", "", ""},
		{"prefers Forwarded over XFF", `for=203.0.113.7`, "203.0.113.7", "198.51.100.42", "198.51.100.42"},
		{"no for= falls through to XFF", `proto=https`, "198.51.100.42", "198.51.100.42", "198.51.100.42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{"Forwarded": tc.header}
			if tc.xffSet != "" {
				headers["X-Forwarded-For"] = tc.xffSet
			}
			got := ExtractClientIP(func(k string) string { return headers[k] }, "10.0.0.5:443")
			if got != tc.want {
				t.Errorf("got %q, want %q (header=%q xff=%q)", got, tc.want, tc.header, tc.xffSet)
			}
		})
	}
}

// TestExtractClientIP_CDNHeaders covers the Cloudflare / Akamai
// fallbacks. Not used on Phase-0 but cheap to validate so the helper
// keeps working when the platform drops a CDN in front of Traefik.
func TestExtractClientIP_CDNHeaders(t *testing.T) {
	cases := []struct {
		header, value, want string
	}{
		{"Cf-Connecting-Ip", "203.0.113.42", "203.0.113.42"},
		{"True-Client-Ip", "198.51.100.7", "198.51.100.7"},
	}
	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			headers := map[string]string{tc.header: tc.value}
			got := ExtractClientIP(func(k string) string { return headers[k] }, "10.0.0.5:443")
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExtractClientIP_PeerAddrFallback ensures the #381 fallback to a
// connection peer addr kicks in when EVERY forwarded header is empty.
// In production this surfaces the in-cluster Traefik pod IP — useless
// for geo (rejected by Lookup as RFC1918) but invaluable as the
// canonical "Traefik forwardedHeaders config is missing" signal the
// stream-opened log line emits.
func TestExtractClientIP_PeerAddrFallback(t *testing.T) {
	got := ExtractClientIP(func(string) string { return "" }, "10.244.1.5:443")
	if got != "10.244.1.5" {
		t.Fatalf("want in-cluster fallback, got %q", got)
	}
}

func TestParseForwardedFor(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"proto=https", ""},
		{"for=203.0.113.7", "203.0.113.7"},
		{` for=203.0.113.7 `, "203.0.113.7"}, // tolerant of surrounding whitespace
		{`for="203.0.113.7"`, "203.0.113.7"},
		{`for="203.0.113.7:54321"`, "203.0.113.7"},
		{`for="[2001:db8::1]:54321"`, "2001:db8::1"},
		{`for="[2001:db8::1]"`, "2001:db8::1"},
		{`for=203.0.113.7, for=10.0.0.5`, "203.0.113.7"},
		{`proto=https;for=203.0.113.7;by=10.0.0.5`, "203.0.113.7"},
		{`FOR=203.0.113.7`, "203.0.113.7"},
		// Malformed but tolerant: missing closing bracket. We strip the
		// '[' so the caller's Lookup rejects the result rather than
		// panicking.
		{`for="[2001:db8::1`, "2001:db8::1"},
	}
	for _, tc := range cases {
		got := parseForwardedFor(tc.in)
		if got != tc.want {
			t.Errorf("parseForwardedFor(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// --- makeRegionSlug --------------------------------------------------------

func TestMakeRegionSlug(t *testing.T) {
	cases := []struct {
		country, region, want string
	}{
		{"TR", "Istanbul", "tr-istanbul"},
		{"US", "California", "us-california"},
		{"US", "", "us"},
		{"DE", "Berlin Stadt", "de-berlin-stadt"},
		{"DE", "Baden-Württemberg", "de-baden-w-rttemberg"}, // non-ascii collapsed
		{"", "Anywhere", ""},
		{"GB", "  ", "gb"},
		{"NL", "Noord-Holland", "nl-noord-holland"},
	}
	for _, tc := range cases {
		got := makeRegionSlug(tc.country, tc.region)
		if got != tc.want {
			t.Errorf("makeRegionSlug(%q,%q) = %q; want %q", tc.country, tc.region, got, tc.want)
		}
	}
}

// --- NoopLookuper ----------------------------------------------------------

func TestNoopLookuper(t *testing.T) {
	var l Lookuper = NoopLookuper{}
	r, err := l.Lookup("203.0.113.7")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if (r != Result{}) {
		t.Fatalf("want zero result, got %+v", r)
	}
}

// --- New(path) -------------------------------------------------------------

func TestNewEmptyPathUnavailable(t *testing.T) {
	_, err := New("")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestNewMissingFileError(t *testing.T) {
	_, err := New("/nonexistent/path/dbip.mmdb")
	if err == nil {
		t.Fatal("want error for missing file")
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing file should not be ErrUnavailable: %v", err)
	}
}

