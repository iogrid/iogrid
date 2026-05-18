package slo

import (
	"strings"
	"testing"
)

// TestRealCatalogue_LoadsAndMatchesSpec walks the actual ./slo
// directory next to the binary at runtime so the unit suite catches
// any drift between the YAML files we ship and the spec the
// telemetry-svc spec mandates (proxy availability, dispatch latency,
// web TTI, magic-link delivery).
func TestRealCatalogue_LoadsAndMatchesSpec(t *testing.T) {
	cat, err := Load("../../slo")
	if err != nil {
		t.Fatalf("Load real catalogue: %v", err)
	}

	want := map[string]float64{
		"proxy-gateway/availability":     99.9,
		"workloads-svc/dispatch-latency": 99,
		"web/tti":                        99,
		"identity-svc/magic-link-delivery": 95,
	}
	got := map[string]float64{}
	for _, s := range cat.SLOs {
		got[s.Service+"/"+s.Name] = s.Objective
	}
	for k, wantObj := range want {
		gotObj, ok := got[k]
		if !ok {
			t.Errorf("missing SLO %s", k)
			continue
		}
		if gotObj != wantObj {
			t.Errorf("SLO %s objective = %g, want %g", k, gotObj, wantObj)
		}
	}

	// Sanity: every SLO must produce a non-empty PrometheusRule.
	rules := cat.PrometheusRules("iogrid")
	if len(rules) != len(cat.SLOs) {
		t.Errorf("PrometheusRules count = %d, want %d", len(rules), len(cat.SLOs))
	}
	out, err := MarshalPrometheusRules(rules)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), "SLOBurnRate_proxy-gateway_availability_page") {
		t.Errorf("rendered rules missing the page-tier proxy-gateway alert")
	}
}
