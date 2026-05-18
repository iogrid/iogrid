// Package xcode lists the Xcode toolchain versions the build-gateway is
// willing to dispatch to Mac providers.
//
// This list is intentionally short and curated. Customers may pin a build to
// any version we publish here; anything else is rejected at submission time
// so we don't waste a provider's wall-clock on a "no matching toolchain"
// failure mid-build.
//
// The set is sourced from cirruslabs/macos-* Tart images we know are kept
// fresh: https://github.com/cirruslabs/macos-image-templates.
package xcode

import (
	"sort"
	"strings"
)

// approved is the set of currently dispatchable Xcode versions. Keep the
// list ordered newest-first so the default (first element) lines up with
// "what most modern projects build against".
var approved = map[string]string{
	"16.2":   "ghcr.io/cirruslabs/macos-sequoia-xcode:16.2",
	"16.1":   "ghcr.io/cirruslabs/macos-sequoia-xcode:16.1",
	"16.0":   "ghcr.io/cirruslabs/macos-sequoia-xcode:16.0",
	"15.4":   "ghcr.io/cirruslabs/macos-sonoma-xcode:15.4",
	"15.3":   "ghcr.io/cirruslabs/macos-sonoma-xcode:15.3",
	"15.2":   "ghcr.io/cirruslabs/macos-sonoma-xcode:15.2",
	"latest": "ghcr.io/cirruslabs/macos-sequoia-xcode:latest",
}

// DefaultVersion is the version the gateway uses when a customer leaves the
// field blank. Matches the "latest" alias so we follow upstream automatically.
const DefaultVersion = "latest"

// IsApproved reports whether v is a version the gateway will dispatch.
// Comparison is case-insensitive and trims whitespace, since the value comes
// from a customer-supplied JSON field.
func IsApproved(v string) bool {
	_, ok := approved[normalize(v)]
	return ok
}

// TartImage returns the canonical Tart image slug for the given approved
// version. Returns ("", false) for unknown versions.
func TartImage(v string) (string, bool) {
	img, ok := approved[normalize(v)]
	return img, ok
}

// Approved returns a stable, sorted snapshot of the approved version strings.
// Used by the surface-area docs / API discovery endpoint.
func Approved() []string {
	out := make([]string, 0, len(approved))
	for k := range approved {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func normalize(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}
