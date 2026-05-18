package registry

import (
	"context"
	"testing"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		in        string
		wantHost  string
		wantNS    string
		wantPath  string
	}{
		{"nginx", "docker.io", "library", "nginx"},
		{"nginx:1.27", "docker.io", "library", "nginx"},
		{"bitnami/redis", "docker.io", "bitnami", "redis"},
		{"bitnami/redis:7", "docker.io", "bitnami", "redis"},
		{"ghcr.io/iogrid/daemon:0.1", "ghcr.io", "iogrid", "daemon"},
		{"quay.io/cilium/cilium@sha256:abc", "quay.io", "cilium", "cilium"},
	}
	for _, tc := range cases {
		host, ns, path, err := ParseRef(tc.in)
		if err != nil {
			t.Errorf("ParseRef(%q) err = %v", tc.in, err)
			continue
		}
		if host != tc.wantHost || ns != tc.wantNS || path != tc.wantPath {
			t.Errorf("ParseRef(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tc.in, host, ns, path, tc.wantHost, tc.wantNS, tc.wantPath)
		}
	}
}

func TestParseRef_Empty(t *testing.T) {
	if _, _, _, err := ParseRef(""); err == nil {
		t.Error("ParseRef(\"\") should error")
	}
}

func TestPolicy_DefaultsAllowKnown(t *testing.T) {
	p := NewDefaultPolicy()
	allowed := []string{
		"ghcr.io/iogrid/daemon:0.1",
		"docker.io/library/nginx:1.27",
		"nginx:1.27",
		"bitnami/redis:7",
		"quay.io/cilium/cilium:v1.16",
		"registry.k8s.io/coredns/coredns:v1.11.1",
	}
	for _, r := range allowed {
		if d := p.Check(r); !d.Allowed {
			t.Errorf("Check(%q) blocked: %s — %s", r, d.Slug, d.Explanation)
		}
	}
}

func TestPolicy_DefaultsDenyUnknown(t *testing.T) {
	p := NewDefaultPolicy()
	denied := []string{
		"docker.io/randomuser/sketchy:latest",
		"public.ecr.aws/foo/bar:1",
		"gcr.io/google-containers/pause:3.10",
	}
	for _, r := range denied {
		if d := p.Check(r); d.Allowed {
			t.Errorf("Check(%q) allowed unexpectedly", r)
		}
	}
}

func TestPolicy_Approve(t *testing.T) {
	p := NewDefaultPolicy()
	if d := p.Check("public.ecr.aws/iogrid/foo:1"); d.Allowed {
		t.Fatal("ecr should not be allowed by default")
	}
	p.Approve("public.ecr.aws", "iogrid")
	if d := p.Check("public.ecr.aws/iogrid/foo:1"); !d.Allowed {
		t.Errorf("after Approve, Check should ALLOW")
	}
}

func TestNoopScanner(t *testing.T) {
	v, err := NoopScanner{}.Scan(context.Background(), "anything")
	if err != nil {
		t.Errorf("NoopScanner.Scan err = %v", err)
	}
	if len(v) != 0 {
		t.Errorf("NoopScanner.Scan returned %d findings, want 0", len(v))
	}
}
