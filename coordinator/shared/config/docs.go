// Package config exposes shared, env-var-overridable runtime configuration
// helpers consumed across iogrid coordinator microservices.
//
// All knobs read here MUST also appear in the canonical names table in
// `docs/RUNBOOKS.md` §5 ("Coordinator env-var contract"). If you add a
// new entry, document it in the same PR — otherwise an operator who
// re-deploys to a private Sovereign has no way to know the override
// exists.
package config

import (
	"net/url"
	"os"
	"strings"
)

// DefaultDocsBaseURL is the canonical public documentation site for iogrid.
// Private-Sovereign operators override this via the `IOGRID_DOCS_BASE_URL`
// env var so that alert `runbook_url` annotations + onboarding-guide links
// point at their own mirrored docs instead of leaking to docs.iogrid.org.
//
// Keep this constant — do NOT change the default. The override path is the
// env var.
const DefaultDocsBaseURL = "https://docs.iogrid.org"

// DocsBaseURLEnv is the env var name. Exported so callers (Helm templates,
// ConfigMap manifests, fail-fast wiring) can reference one symbol.
const DocsBaseURLEnv = "IOGRID_DOCS_BASE_URL"

// DocsBaseURL returns the configured base URL for the documentation site
// with any trailing slash stripped. The value comes from the
// `IOGRID_DOCS_BASE_URL` env var; if unset or empty it falls back to the
// public site (`DefaultDocsBaseURL`).
//
// We strip the trailing slash so callers can do `base + "/runbooks/foo"`
// without worrying about double-slashing.
func DocsBaseURL() string {
	v := strings.TrimSpace(os.Getenv(DocsBaseURLEnv))
	if v == "" {
		v = DefaultDocsBaseURL
	}
	return strings.TrimRight(v, "/")
}

// DocsURL joins the configured docs base URL with the given path segments.
// Each segment is appended directly (callers control the slashes inside a
// segment, e.g. `#anchor`) — only the boundary between segments is
// normalised. The leading slash on the joined path is guaranteed.
//
// Example:
//
//	DocsURL("runbooks", "antiabuse-spike")
//	  → "https://docs.iogrid.org/runbooks/antiabuse-spike"
//	DocsURL("runbooks/slo-burn-rate#identity-svc-magic-link-delivery")
//	  → "https://docs.iogrid.org/runbooks/slo-burn-rate#identity-svc-magic-link-delivery"
func DocsURL(segments ...string) string {
	base := DocsBaseURL()
	if len(segments) == 0 {
		return base
	}
	parts := make([]string, 0, len(segments))
	for _, s := range segments {
		s = strings.Trim(s, "/")
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return base
	}
	return base + "/" + strings.Join(parts, "/")
}

// RunbookURL is the canonical helper for alert `runbook_url` annotations
// and operator-facing runbook links. `slug` is the path segment(s) under
// `/runbooks/`, e.g. `"antiabuse-spike"` or
// `"slo-burn-rate#identity-svc-magic-link-delivery"`.
//
// Use this in preference to hand-rolled fmt.Sprintf calls — that's the
// pattern that caused #418 (5 hardcoded `https://docs.iogrid.org/runbooks/…`
// sites across telemetry-svc + gateway-bff that broke for any operator
// pointing their Sovereign at a private docs mirror).
func RunbookURL(slug string) string {
	slug = strings.TrimLeft(slug, "/")
	return DocsURL("runbooks") + "/" + slug
}

// validateBaseURL is a defensive helper kept private; we don't fail-fast on
// a malformed override at startup because (a) the value is rendered into
// alert annotations + JSON responses where a malformed URL surfaces
// immediately, and (b) we don't want a typo in a private-Sovereign env
// var to crash an entire service. If a caller wants to validate, expose
// this via a Check function in the future.
func validateBaseURL(s string) bool { //nolint:unused // reserved for future Check()
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != ""
}
