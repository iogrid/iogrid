package slo

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// BurnRate is one Multi-Window Multi-Burn-Rate alert pair from the
// Google SRE Workbook. ShortWindow and LongWindow MUST both exceed
// the threshold simultaneously before the alert fires — that's the
// MWMB trick that suppresses false pages on a single brief outage.
type BurnRate struct {
	// Name is the alert name slug.
	Name string
	// LongWindow is the "wide" lookback (e.g. 1h).
	LongWindow string
	// ShortWindow is the "narrow" lookback (e.g. 5m).
	ShortWindow string
	// Threshold is the burn-rate multiplier (e.g. 14.4 = consuming
	// the monthly budget 14.4x faster than allowed).
	Threshold float64
	// Severity is "page" or "warn" — drives Alertmanager routing.
	Severity string
}

// DefaultBurnRates returns the two-tier MWMB schedule the spec calls
// out: page on 14x, warn on 2x.
func DefaultBurnRates() []BurnRate {
	return []BurnRate{
		{Name: "page", LongWindow: "1h", ShortWindow: "5m", Threshold: 14.4, Severity: "page"},
		{Name: "warn", LongWindow: "6h", ShortWindow: "30m", Threshold: 2, Severity: "warn"},
	}
}

// PrometheusRule is a minimal stand-in for the operator's
// monitoring.coreos.com/v1 PrometheusRule CRD. We avoid pulling the
// real CRD's Go module because it carries a heavy k8s.io dependency
// tree (apimachinery etc.) and we only need to emit YAML — Flux /
// kubectl handle validation downstream.
type PrometheusRule struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   map[string]interface{} `yaml:"metadata"`
	Spec       PrometheusRuleSpec     `yaml:"spec"`
}

// PrometheusRuleSpec wraps a list of rule groups.
type PrometheusRuleSpec struct {
	Groups []RuleGroup `yaml:"groups"`
}

// RuleGroup is one Prometheus rule-group.
type RuleGroup struct {
	Name     string `yaml:"name"`
	Interval string `yaml:"interval"`
	Rules    []Rule `yaml:"rules"`
}

// Rule covers both recording and alerting rules; the CRD's discriminator
// is which of `record` / `alert` is set.
type Rule struct {
	Record      string            `yaml:"record,omitempty"`
	Alert       string            `yaml:"alert,omitempty"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

// PrometheusRules emits one PrometheusRule object per SLO. The returned
// slice is yaml.Marshal-able as a multi-doc YAML stream via
// MarshalPrometheusRules().
func (c *Catalogue) PrometheusRules(namespace string) []PrometheusRule {
	out := make([]PrometheusRule, 0, len(c.SLOs))
	for _, s := range c.SLOs {
		out = append(out, buildPromRule(s, namespace, DefaultBurnRates()))
	}
	return out
}

// MarshalPrometheusRules returns the multi-doc YAML stream for the
// supplied PrometheusRule objects. Suitable for piping to
// `kubectl apply -f -`.
func MarshalPrometheusRules(rules []PrometheusRule) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, r := range rules {
		if err := enc.Encode(r); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildPromRule(s SLO, namespace string, burns []BurnRate) PrometheusRule {
	budget := s.ErrorBudget()

	// Recording rules: pre-compute the burn rate over each window so
	// the alert query stays cheap.
	//
	// SLI semantics: `good` and `total` are FULLY-FORMED PromQL exprs
	// returning a rated counter (events / second). The recording rule
	// re-rates them over the burn window — equivalent to
	// (1 - increase(good) / increase(total)) for that window. Wrapping
	// in a subquery `[<window>:]` is the PromQL-1.x way to express
	// "average this expression over that window".
	rules := []Rule{}
	for _, b := range burns {
		// observed error ratio over <window> = 1 - (avg good / avg total)
		shortExpr := fmt.Sprintf("1 - (avg_over_time((%s)[%s:]) / avg_over_time((%s)[%s:]))",
			s.SLI.Good, b.ShortWindow, s.SLI.Total, b.ShortWindow)
		longExpr := fmt.Sprintf("1 - (avg_over_time((%s)[%s:]) / avg_over_time((%s)[%s:]))",
			s.SLI.Good, b.LongWindow, s.SLI.Total, b.LongWindow)

		shortRecord := fmt.Sprintf("slo:burn_rate:short:%s:%s:%s", s.Service, s.Name, b.Name)
		longRecord := fmt.Sprintf("slo:burn_rate:long:%s:%s:%s", s.Service, s.Name, b.Name)

		rules = append(rules,
			Rule{Record: shortRecord, Expr: shortExpr},
			Rule{Record: longRecord, Expr: longExpr},
		)

		alertName := fmt.Sprintf("SLOBurnRate_%s_%s_%s", s.Service, s.Name, b.Name)
		alertExpr := fmt.Sprintf("%s > (%.4g * %.6g) and %s > (%.4g * %.6g)",
			shortRecord, b.Threshold, budget, longRecord, b.Threshold, budget)
		labels := map[string]string{
			"severity":      b.Severity,
			"slo_service":   s.Service,
			"slo_name":      s.Name,
			"burn_rate":     b.Name,
			"slo_objective": fmt.Sprintf("%.4g", s.Objective),
		}
		for k, v := range s.Labels {
			labels[k] = v
		}
		annotations := map[string]string{
			"summary":     fmt.Sprintf("%s/%s burning error budget %.4gx (%s severity)", s.Service, s.Name, b.Threshold, b.Severity),
			"description": s.Description,
			"runbook_url": fmt.Sprintf("https://docs.iogrid.org/runbooks/slo-burn-rate#%s-%s", s.Service, s.Name),
		}
		for k, v := range s.Annotations {
			annotations[k] = v
		}
		rules = append(rules, Rule{
			Alert:       alertName,
			Expr:        alertExpr,
			For:         "2m",
			Labels:      labels,
			Annotations: annotations,
		})
	}

	return PrometheusRule{
		APIVersion: "monitoring.coreos.com/v1",
		Kind:       "PrometheusRule",
		Metadata: map[string]interface{}{
			"name":      fmt.Sprintf("iogrid-slo-%s-%s", s.Service, s.Name),
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/name":      "telemetry-svc",
				"app.kubernetes.io/part-of":   "iogrid",
				"app.kubernetes.io/component": "slo",
				"slo.iogrid.org/service":      s.Service,
				"slo.iogrid.org/name":         s.Name,
			},
		},
		Spec: PrometheusRuleSpec{
			Groups: []RuleGroup{{
				Name:     fmt.Sprintf("iogrid.slo.%s.%s", s.Service, s.Name),
				Interval: "30s",
				Rules:    rules,
			}},
		},
	}
}
