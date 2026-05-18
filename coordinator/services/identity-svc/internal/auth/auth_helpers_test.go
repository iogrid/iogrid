package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

func TestHumanDuration_Common(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{10 * time.Minute, "10 minutes"},
		{2 * time.Hour, "2 hours"},
		{45 * time.Second, "45s"},
	}
	for _, c := range cases {
		got := humanDuration(c.in)
		if got != c.want {
			t.Errorf("humanDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDedup_PreservesOrderAndDrops(t *testing.T) {
	got := dedup([]string{"a", "b", "a", "", "c", "b"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("idx %d: got %q want %q", i, got[i], v)
		}
	}
}

func TestIdentifierKindsToStrings_DistinctOrder(t *testing.T) {
	// nil-safe.
	out := identifierKindsToStrings(nil)
	if len(out) != 0 {
		t.Errorf("expected empty, got %v", out)
	}
}

func TestService_CheckReturnTo_AllowsRelative(t *testing.T) {
	svc := New(Options{BaseURL: "http://x", AllowedReturnHosts: []string{"iogrid.org"}})
	if err := svc.checkReturnTo("/foo"); err != nil {
		t.Errorf("relative URL rejected: %v", err)
	}
	if err := svc.checkReturnTo(""); err != nil {
		t.Errorf("empty rejected: %v", err)
	}
}

func TestService_CheckReturnTo_AllowsListedHost(t *testing.T) {
	svc := New(Options{BaseURL: "http://x", AllowedReturnHosts: []string{"iogrid.org"}})
	if err := svc.checkReturnTo("https://iogrid.org/app"); err != nil {
		t.Errorf("listed host rejected: %v", err)
	}
}

func TestService_CheckReturnTo_RejectsUnlistedHost(t *testing.T) {
	svc := New(Options{BaseURL: "http://x", AllowedReturnHosts: []string{"iogrid.org"}})
	if err := svc.checkReturnTo("https://attacker.example/steal"); err == nil {
		t.Errorf("attacker host should be rejected")
	}
}

func TestNew_AppliesDefaults(t *testing.T) {
	svc := New(Options{})
	if svc.MagicLinkTTL != 10*time.Minute {
		t.Errorf("MagicLinkTTL: %v", svc.MagicLinkTTL)
	}
	if svc.RefreshTokenTTL != 30*24*time.Hour {
		t.Errorf("RefreshTokenTTL: %v", svc.RefreshTokenTTL)
	}
	if svc.StepUpTTL != 5*time.Minute {
		t.Errorf("StepUpTTL: %v", svc.StepUpTTL)
	}
	if svc.MagicLinkPerEmailPerHour != 3 {
		t.Errorf("MagicLinkPerEmailPerHour: %d", svc.MagicLinkPerEmailPerHour)
	}
	if svc.MagicLinkPerIPPerHour != 10 {
		t.Errorf("MagicLinkPerIPPerHour: %d", svc.MagicLinkPerIPPerHour)
	}
	if svc.Logger == nil {
		t.Errorf("Logger must default to slog.Default")
	}
}

func TestNew_NormalisesAllowedReturnHosts(t *testing.T) {
	svc := New(Options{AllowedReturnHosts: []string{"  IOGRID.ORG  ", "App.iogrid.org"}})
	if _, ok := svc.AllowedReturnHosts["iogrid.org"]; !ok {
		t.Errorf("AllowedReturnHosts not lower-cased + trimmed: %v", svc.AllowedReturnHosts)
	}
	if _, ok := svc.AllowedReturnHosts["app.iogrid.org"]; !ok {
		t.Errorf("AllowedReturnHosts second entry missing: %v", svc.AllowedReturnHosts)
	}
}

func TestBuildMagicLinkURL_EncodesQuery(t *testing.T) {
	svc := New(Options{BaseURL: "https://api.iogrid.org", AllowedReturnHosts: []string{"iogrid.org"}})
	u := svc.buildMagicLinkURL("ab+cd/ef==", "https://iogrid.org/x")
	if !strings.Contains(u, "/v1/auth/magic-link/complete") {
		t.Errorf("URL missing path: %q", u)
	}
	if !strings.Contains(u, "token=") {
		t.Errorf("URL missing token query: %q", u)
	}
	if !strings.Contains(u, "return_to=") {
		t.Errorf("URL missing return_to query: %q", u)
	}
}

func TestBuildMagicLinkURL_StripsTrailingSlashFromBase(t *testing.T) {
	svc := New(Options{BaseURL: "https://api.iogrid.org/"})
	u := svc.buildMagicLinkURL("xx", "")
	if strings.Contains(u, "//v1") {
		t.Errorf("URL has double slash: %q", u)
	}
}

func TestRequestMetadata_HandlesNil(t *testing.T) {
	ip, ua := requestMetadata(nil)
	if ip != nil || ua != "" {
		t.Errorf("nil req should return zero values; got ip=%v ua=%q", ip, ua)
	}
}

func TestRequestMetadata_ParsesXRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Real-IP", "203.0.113.7")
	req.Header.Set("User-Agent", "go-test")
	ip, ua := requestMetadata(req)
	if ip == nil || ip.String() != "203.0.113.7" {
		t.Errorf("ip: %v", ip)
	}
	if ua != "go-test" {
		t.Errorf("ua: %q", ua)
	}
}

func TestIdentifierKindsToStrings(t *testing.T) {
	out := identifierKindsToStrings([]store.Identifier{
		{Kind: store.KindGoogle},
		{Kind: store.KindMagicLink},
	})
	if len(out) != 2 || out[0] != "google" || out[1] != "magic_link" {
		t.Errorf("unexpected kinds: %v", out)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	if !isUniqueViolation(&pgconn.PgError{Code: "23505"}) {
		t.Errorf("23505 should be a unique violation")
	}
	if isUniqueViolation(errors.New("plain")) {
		t.Errorf("plain error should not be flagged")
	}
	if isUniqueViolation(&pgconn.PgError{Code: "42P01"}) {
		t.Errorf("42P01 should not be a unique violation")
	}
}

// TestRequestMagicLink_RejectsBadEmail exercises the input-validation
// branch which doesn't need a store/limiter.
func TestRequestMagicLink_RejectsBadEmail(t *testing.T) {
	svc := New(Options{})
	if _, err := svc.RequestMagicLink(context.Background(), "", "", "", store.IntentSignIn); err == nil {
		t.Errorf("empty email should fail")
	}
	if _, err := svc.RequestMagicLink(context.Background(), "noatsign", "", "", store.IntentSignIn); err == nil {
		t.Errorf("invalid email should fail")
	}
}

// TestRequestMagicLink_RejectsBadReturnTo exercises the open-redirect
// guard before any store / limiter call.
func TestRequestMagicLink_RejectsBadReturnTo(t *testing.T) {
	svc := New(Options{AllowedReturnHosts: []string{"iogrid.org"}})
	if _, err := svc.RequestMagicLink(context.Background(), "x@y.z", "https://attacker.example/", "", store.IntentSignIn); err == nil {
		t.Errorf("attacker return_to should fail")
	}
}

// TestCompleteMagicLink_RejectsEmpty exercises the no-store empty-token
// branch.
func TestCompleteMagicLink_RejectsEmpty(t *testing.T) {
	svc := New(Options{})
	if _, err := svc.CompleteMagicLink(context.Background(), "", nil); err == nil {
		t.Errorf("empty token should fail")
	}
}

// TestRefresh_RejectsEmpty exercises the no-store empty-token branch.
func TestRefresh_RejectsEmpty(t *testing.T) {
	svc := New(Options{})
	if _, err := svc.Refresh(context.Background(), "", nil); err == nil {
		t.Errorf("empty refresh token should fail")
	}
}
