// Package slo parses the SLO catalogue under slo/*.yaml and exposes the
// burn-rate posture via the /status endpoint.
//
// The on-disk format is intentionally Sloth-compatible (sloth.salvador.io)
// so the same files can be fed straight into `sloth generate` to emit
// PrometheusRule CRDs. We keep a hand-written generator alongside
// (PrometheusRules()) so a vanilla Prometheus deployment without the
// Sloth operator still gets the alerting rules.
//
// Burn-rate windows follow the Google SRE Workbook table:
//
//	1h  / 5m   — 14.4x burn — page on-call
//	6h  / 30m  — 6x   burn — page on-call
//	1d  / 2h   — 3x   burn — ticket
//	3d  / 6h   — 1x   burn — ticket
//
// We collapse to the two extremes the spec asked for: 14x (page) and 2x
// (warn). Adding more windows is a one-line append to BurnRates.
package slo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SLO is one service-level objective. Marshalling matches the Sloth v1
// schema so the same YAML files round-trip through `sloth generate`.
type SLO struct {
	// Service is the iogrid microservice the SLO covers
	// (e.g. proxy-gateway, identity-svc).
	Service string `yaml:"service"`
	// Name is a stable slug used as the alert name suffix.
	Name string `yaml:"name"`
	// Description is shown in Grafana / Alertmanager.
	Description string `yaml:"description"`
	// Objective is the target ratio, e.g. 99.9 means "99.9% must be
	// good over the time window".
	Objective float64 `yaml:"objective"`
	// TimeWindow is "30d", "7d", "1h". Sloth interprets the suffix.
	TimeWindow string `yaml:"time_window"`
	// SLI defines the good-events / total-events ratio in Prometheus
	// query terms.
	SLI SLI `yaml:"sli"`
	// Labels are stamped on every emitted PrometheusRule.
	Labels map[string]string `yaml:"labels"`
	// Annotations are stamped on every alert rule.
	Annotations map[string]string `yaml:"annotations"`
}

// SLI describes how to extract the good/total counts from Prometheus.
type SLI struct {
	// Good is a PromQL counter (the SUM of "good" events).
	Good string `yaml:"good"`
	// Total is the PromQL counter for "all events" — denominator.
	Total string `yaml:"total"`
}

// Catalogue is the deserialised SLO set. Build via Load().
type Catalogue struct {
	SLOs []SLO
}

// Load reads every *.yaml file under dir (non-recursive) and decodes
// it as either a single SLO doc or a list document.
//
// Files starting with "_" are skipped — convention for partials.
func Load(dir string) (*Catalogue, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read SLO dir: %w", err)
	}
	cat := &Catalogue{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "_") {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		slos, err := decode(data)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		cat.SLOs = append(cat.SLOs, slos...)
	}
	sort.Slice(cat.SLOs, func(i, j int) bool {
		if cat.SLOs[i].Service != cat.SLOs[j].Service {
			return cat.SLOs[i].Service < cat.SLOs[j].Service
		}
		return cat.SLOs[i].Name < cat.SLOs[j].Name
	})
	if err := cat.Validate(); err != nil {
		return nil, err
	}
	return cat, nil
}

// decode handles both shapes: a single SLO doc or {slos: [SLO...]}.
func decode(data []byte) ([]SLO, error) {
	// Try the list form first.
	var doc struct {
		SLOs []SLO `yaml:"slos"`
	}
	if err := yaml.Unmarshal(data, &doc); err == nil && len(doc.SLOs) > 0 {
		return doc.SLOs, nil
	}
	// Fall back to a single SLO doc.
	var single SLO
	if err := yaml.Unmarshal(data, &single); err != nil {
		return nil, err
	}
	if single.Service == "" && single.Name == "" {
		return nil, nil
	}
	return []SLO{single}, nil
}

// Validate ensures every SLO has the required fields. Catches typos
// before the rule generator emits a broken PrometheusRule that
// Prometheus would silently load with no alerts firing.
func (c *Catalogue) Validate() error {
	seen := map[string]bool{}
	for i, s := range c.SLOs {
		if s.Service == "" {
			return fmt.Errorf("slo[%d]: service is required", i)
		}
		if s.Name == "" {
			return fmt.Errorf("slo[%d]: name is required", i)
		}
		if s.Objective <= 0 || s.Objective >= 100 {
			return fmt.Errorf("slo[%d] %s/%s: objective must be in (0, 100)", i, s.Service, s.Name)
		}
		if s.TimeWindow == "" {
			return fmt.Errorf("slo[%d] %s/%s: time_window is required", i, s.Service, s.Name)
		}
		if s.SLI.Good == "" || s.SLI.Total == "" {
			return fmt.Errorf("slo[%d] %s/%s: sli.good and sli.total are required", i, s.Service, s.Name)
		}
		key := s.Service + "/" + s.Name
		if seen[key] {
			return fmt.Errorf("duplicate SLO: %s", key)
		}
		seen[key] = true
	}
	return nil
}

// ErrorBudget returns the allowed failure ratio (1 - objective/100).
// A 99.9% SLO has a 0.001 error budget.
func (s SLO) ErrorBudget() float64 {
	return 1 - s.Objective/100
}
