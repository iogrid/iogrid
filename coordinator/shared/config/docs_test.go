package config

import "testing"

func TestDocsBaseURL_DefaultWhenUnset(t *testing.T) {
	t.Setenv(DocsBaseURLEnv, "")
	if got, want := DocsBaseURL(), DefaultDocsBaseURL; got != want {
		t.Fatalf("DocsBaseURL() with unset env = %q, want %q", got, want)
	}
}

func TestDocsBaseURL_OverrideStripsTrailingSlash(t *testing.T) {
	t.Setenv(DocsBaseURLEnv, "https://docs.mygrid.io/")
	if got, want := DocsBaseURL(), "https://docs.mygrid.io"; got != want {
		t.Fatalf("DocsBaseURL() = %q, want %q", got, want)
	}
}

func TestDocsBaseURL_OverrideHonoured(t *testing.T) {
	t.Setenv(DocsBaseURLEnv, "https://docs.private.example/iogrid")
	if got, want := DocsBaseURL(), "https://docs.private.example/iogrid"; got != want {
		t.Fatalf("DocsBaseURL() = %q, want %q", got, want)
	}
}

func TestDocsURL_JoinsSegments(t *testing.T) {
	t.Setenv(DocsBaseURLEnv, "")
	tests := []struct {
		name     string
		segments []string
		want     string
	}{
		{"no segments returns base", nil, "https://docs.iogrid.org"},
		{"single segment", []string{"runbooks"}, "https://docs.iogrid.org/runbooks"},
		{"two segments", []string{"runbooks", "antiabuse-spike"}, "https://docs.iogrid.org/runbooks/antiabuse-spike"},
		{"strips inner slashes", []string{"/runbooks/", "/foo/"}, "https://docs.iogrid.org/runbooks/foo"},
		{"empty segments skipped", []string{"runbooks", "", "foo"}, "https://docs.iogrid.org/runbooks/foo"},
		{"hash in segment preserved", []string{"runbooks/slo-burn-rate#id-svc"}, "https://docs.iogrid.org/runbooks/slo-burn-rate#id-svc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DocsURL(tc.segments...); got != tc.want {
				t.Fatalf("DocsURL(%v) = %q, want %q", tc.segments, got, tc.want)
			}
		})
	}
}

func TestRunbookURL(t *testing.T) {
	t.Setenv(DocsBaseURLEnv, "")
	tests := []struct {
		name string
		slug string
		want string
	}{
		{"plain slug", "antiabuse-spike", "https://docs.iogrid.org/runbooks/antiabuse-spike"},
		{"slug with anchor", "slo-burn-rate#identity-svc-magic-link-delivery", "https://docs.iogrid.org/runbooks/slo-burn-rate#identity-svc-magic-link-delivery"},
		{"leading slash trimmed", "/provider-bandwidth-spike", "https://docs.iogrid.org/runbooks/provider-bandwidth-spike"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RunbookURL(tc.slug); got != tc.want {
				t.Fatalf("RunbookURL(%q) = %q, want %q", tc.slug, got, tc.want)
			}
		})
	}
}

func TestRunbookURL_OverrideAppliesToRunbooks(t *testing.T) {
	t.Setenv(DocsBaseURLEnv, "https://docs.mygrid.io")
	got := RunbookURL("service-down")
	want := "https://docs.mygrid.io/runbooks/service-down"
	if got != want {
		t.Fatalf("RunbookURL with override = %q, want %q", got, want)
	}
}
