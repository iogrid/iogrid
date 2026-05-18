package slo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_SingleDoc(t *testing.T) {
	dir := t.TempDir()
	yaml := `service: proxy-gateway
name: availability
description: Proxy-gateway availability over 30 days.
objective: 99.9
time_window: 30d
sli:
  good: sum(rate(http_requests_total{service="proxy-gateway",code!~"5.."}[5m]))
  total: sum(rate(http_requests_total{service="proxy-gateway"}[5m]))
labels:
  team: platform
annotations:
  summary: proxy-gateway availability
`
	if err := os.WriteFile(filepath.Join(dir, "proxy.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cat.SLOs) != 1 {
		t.Fatalf("want 1 SLO, got %d", len(cat.SLOs))
	}
	if cat.SLOs[0].Service != "proxy-gateway" {
		t.Errorf("service = %q", cat.SLOs[0].Service)
	}
	if got := cat.SLOs[0].ErrorBudget(); got < 0.0009 || got > 0.0011 {
		t.Errorf("ErrorBudget = %f, want ~0.001", got)
	}
}

func TestLoad_ListDoc(t *testing.T) {
	dir := t.TempDir()
	yaml := `slos:
  - service: identity-svc
    name: magic-link-delivery
    description: Magic-link delivery within 30s
    objective: 95
    time_window: 30d
    sli:
      good: sum(rate(magic_link_delivered_under_30s_total[5m]))
      total: sum(rate(magic_link_requested_total[5m]))
  - service: workloads-svc
    name: dispatch-latency
    description: Workload dispatch p95 < 100ms
    objective: 99
    time_window: 30d
    sli:
      good: sum(rate(workloads_dispatch_under_100ms_total[5m]))
      total: sum(rate(workloads_dispatch_total[5m]))
`
	if err := os.WriteFile(filepath.Join(dir, "multi.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cat.SLOs) != 2 {
		t.Fatalf("want 2 SLOs, got %d", len(cat.SLOs))
	}
	// Sorted by service alphabetically.
	if cat.SLOs[0].Service != "identity-svc" {
		t.Errorf("first SLO service = %q", cat.SLOs[0].Service)
	}
}

func TestLoad_Validation(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"missing service", `name: foo
objective: 99
time_window: 30d
sli: {good: g, total: t}`, "service is required"},
		{"missing name", `service: svc
objective: 99
time_window: 30d
sli: {good: g, total: t}`, "name is required"},
		{"objective out of range", `service: svc
name: n
objective: 100
time_window: 30d
sli: {good: g, total: t}`, "objective must be in (0, 100)"},
		{"missing sli", `service: svc
name: n
objective: 99
time_window: 30d
sli: {}`, "sli.good and sli.total are required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sub := filepath.Join(dir, c.name)
			_ = os.MkdirAll(sub, 0o755)
			_ = os.WriteFile(filepath.Join(sub, "s.yaml"), []byte(c.yaml), 0o644)
			_, err := Load(sub)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestLoad_IgnoresPartialsAndNonYaml(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "_partial.yaml"), []byte("nope: nope"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("not yaml"), 0o644)
	cat, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cat.SLOs) != 0 {
		t.Errorf("want empty catalogue, got %d", len(cat.SLOs))
	}
}

func TestPrometheusRules_GeneratesAlerts(t *testing.T) {
	cat := &Catalogue{SLOs: []SLO{{
		Service:     "proxy-gateway",
		Name:        "availability",
		Description: "test",
		Objective:   99.9,
		TimeWindow:  "30d",
		SLI:         SLI{Good: "sum(rate(g[5m]))", Total: "sum(rate(t[5m]))"},
	}}}
	rules := cat.PrometheusRules("iogrid")
	if len(rules) != 1 {
		t.Fatalf("want 1 rule object, got %d", len(rules))
	}
	out, err := MarshalPrometheusRules(rules)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"PrometheusRule",
		"SLOBurnRate_proxy-gateway_availability_page",
		"SLOBurnRate_proxy-gateway_availability_warn",
		"severity: page",
		"severity: warn",
		"burn_rate: page",
		"burn_rate: warn",
		"slo:burn_rate:short:proxy-gateway:availability:page",
		"slo:burn_rate:long:proxy-gateway:availability:page",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered rules missing %q\n--- output ---\n%s", want, s)
		}
	}
}

func TestStandaloneRules(t *testing.T) {
	rules := StandaloneRules("iogrid")
	if len(rules) != 1 {
		t.Fatalf("want 1 rule object, got %d", len(rules))
	}
	out, err := MarshalPrometheusRules(rules)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"AntiAbuseHitRateSpike",
		"ProviderBandwidthSpike",
		"CoordinatorServiceDown",
		"temp_suspend",
		"namespace=\"iogrid\"",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("standalone rules missing %q\n--- output ---\n%s", want, s)
		}
	}
}
