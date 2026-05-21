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

