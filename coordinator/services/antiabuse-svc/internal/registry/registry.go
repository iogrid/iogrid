// Package registry enforces the docs/LEGAL.md "Docker workload filters"
// rule #1:
//
//	Container image must come from approved registry (default: ghcr.io,
//	docker.io official-images, Dockerhub-verified-publisher namespace)
//
// This is the lightweight part of CheckContainerImage — purely a
// reference-format and registry-host check. The SBOM / vulnerability
// piece (rule #2) is wired as an interface (Scanner) that today
// returns a no-op; production will plug in osv-scanner or Trivy.
package registry

import (
	"context"
	"errors"
	"strings"
)

// Decision is the outcome of a CheckContainerImage call.
type Decision struct {
	Allowed     bool
	Reason      string
	Slug        string
	Explanation string
}

// Policy lists the approved registries / namespaces.
type Policy struct {
	approved []approvedEntry
}

type approvedEntry struct {
	// host is the registry hostname (e.g. "ghcr.io"). Empty matches
	// the implicit docker.io.
	host string
	// namespace is the "owner" or "library" prefix. Empty matches any.
	namespace string
}

// NewDefaultPolicy seeds the policy with the docs/LEGAL.md defaults.
func NewDefaultPolicy() *Policy {
	return &Policy{approved: []approvedEntry{
		{host: "ghcr.io"},
		// docker.io official images (the "library/..." namespace).
		{host: "docker.io", namespace: "library"},
		// Dockerhub verified-publisher convention (no host-side
		// signal yet; we keep an explicit allow-list of namespaces).
		{host: "docker.io", namespace: "bitnami"},
		{host: "docker.io", namespace: "grafana"},
		{host: "docker.io", namespace: "redis"},
		{host: "docker.io", namespace: "prom"},
		// Kubernetes-native registries iogrid uses internally.
		{host: "registry.k8s.io"},
		{host: "quay.io"},
	}}
}

// Approve appends an additional registry/namespace pair.
func (p *Policy) Approve(host, namespace string) {
	p.approved = append(p.approved, approvedEntry{
		host:      strings.ToLower(strings.TrimSpace(host)),
		namespace: strings.ToLower(strings.TrimSpace(namespace)),
	})
}

// Snapshot returns the policy as human-readable strings (used by
// ListFilters).
func (p *Policy) Snapshot() []string {
	out := make([]string, 0, len(p.approved))
	for _, e := range p.approved {
		s := e.host
		if e.namespace != "" {
			s += "/" + e.namespace
		}
		out = append(out, s)
	}
	return out
}

// Check evaluates an image reference. It returns Allowed=true for any
// reference whose registry+namespace pair is on the approved list.
//
// Reference formats supported:
//
//	docker.io/library/nginx:1.27
//	ghcr.io/iogrid/daemon@sha256:abc...
//	bitnami/redis:7
//	nginx:latest             (bare → docker.io/library/nginx)
//	myorg/myimage:v1         (bare → docker.io/myorg/myimage)
func (p *Policy) Check(imageRef string) Decision {
	host, namespace, _, err := ParseRef(imageRef)
	if err != nil {
		return Decision{
			Allowed: false,
			Reason:  err.Error(),
			Slug:    "image_ref_invalid",
		}
	}
	for _, e := range p.approved {
		if e.host == host && (e.namespace == "" || e.namespace == namespace) {
			return Decision{Allowed: true, Slug: "image_registry_allowed"}
		}
	}
	return Decision{
		Allowed:     false,
		Reason:      "registry_not_approved",
		Slug:        "image_registry_denied",
		Explanation: "registry " + host + "/" + namespace + " not on approved list",
	}
}

// ParseRef splits an image reference into host / namespace / path.
// It implements the same defaults as containerd's reference parser:
// bare names go to docker.io/library/..., a single-slash name with
// no dot in the prefix goes to docker.io/<name>.
func ParseRef(ref string) (host, namespace, path string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", "", errors.New("empty image reference")
	}
	// Drop digest or tag suffix.
	if i := strings.IndexByte(ref, '@'); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndexByte(ref, ':'); i >= 0 {
		// Only treat as a tag if there's no slash after it
		// (otherwise it's a port number like ":5000").
		if !strings.ContainsRune(ref[i:], '/') {
			ref = ref[:i]
		}
	}
	parts := strings.SplitN(ref, "/", 3)
	switch len(parts) {
	case 1:
		// bare "nginx" → docker.io/library/nginx
		return "docker.io", "library", parts[0], nil
	case 2:
		// "foo/bar". If foo looks like a host (has '.' or ':'), it
		// is a host; otherwise it's a docker.io namespace.
		if strings.ContainsAny(parts[0], ".:") {
			return strings.ToLower(parts[0]), parts[1], "", nil
		}
		return "docker.io", strings.ToLower(parts[0]), parts[1], nil
	case 3:
		return strings.ToLower(parts[0]), strings.ToLower(parts[1]), parts[2], nil
	}
	return "", "", "", errors.New("invalid image reference")
}

// Scanner is the SBOM / vulnerability hook. Production will plug in
// osv-scanner; the stub here returns no findings so the rest of the
// pipeline can be tested today.
type Scanner interface {
	// Scan inspects the image and returns any CRITICAL / HIGH
	// vulnerabilities. Empty slice means "all clear".
	Scan(ctx context.Context, imageRef string) ([]Vulnerability, error)
}

// Vulnerability mirrors an osv-scanner finding.
type Vulnerability struct {
	ID       string
	Severity string
	Summary  string
}

// NoopScanner is a Scanner that returns no findings. Used as the
// default until osv-scanner is wired up.
type NoopScanner struct{}

// Scan returns nil, nil.
func (NoopScanner) Scan(ctx context.Context, imageRef string) ([]Vulnerability, error) {
	return nil, nil
}
