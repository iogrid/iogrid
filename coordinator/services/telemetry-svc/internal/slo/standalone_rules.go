package slo

// StandaloneRules covers the rules the spec calls out that aren't tied
// to a per-SLO burn-rate alert:
//
//   - Anti-abuse hit-rate spike (>5x rolling baseline → page abuse team)
//   - Anomalous provider behavior (sudden 100x bandwidth spike → temp-suspend)
//   - Coordinator service down >5min
//
// They live in their own rule group so SLO files stay focused on SLIs.

// StandaloneRules returns the operator-defined PrometheusRule objects
// that are NOT derived from an SLO. namespace is the k8s namespace the
// CRDs land in.
func StandaloneRules(namespace string) []PrometheusRule {
	return []PrometheusRule{
		{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "PrometheusRule",
			Metadata: map[string]interface{}{
				"name":      "iogrid-operations",
				"namespace": namespace,
				"labels": map[string]string{
					"app.kubernetes.io/name":      "telemetry-svc",
					"app.kubernetes.io/part-of":   "iogrid",
					"app.kubernetes.io/component": "operations",
				},
			},
			Spec: PrometheusRuleSpec{
				Groups: []RuleGroup{
					{
						Name:     "iogrid.antiabuse",
						Interval: "30s",
						Rules: []Rule{
							{
								// Hit-rate spike: current 5m rate > 5x rolling 24h baseline.
								// Min raw rate guard (>0.1/s) prevents flapping at low traffic.
								Alert: "AntiAbuseHitRateSpike",
								Expr: `(sum(rate(antiabuse_filter_block_total[5m])) ` +
									`> 0.1) and ` +
									`(sum(rate(antiabuse_filter_block_total[5m])) ` +
									`> 5 * sum(rate(antiabuse_filter_block_total[24h])))`,
								For: "5m",
								Labels: map[string]string{
									"severity": "page",
									"team":     "abuse",
								},
								Annotations: map[string]string{
									"summary":     "Anti-abuse hit rate >5x baseline",
									"description": "Block rate over last 5m exceeds 5x the 24h rolling baseline — possible coordinated abuse, credential stuffing, or filter misconfiguration.",
									"runbook_url": "https://docs.iogrid.org/runbooks/antiabuse-spike",
								},
							},
						},
					},
					{
						Name:     "iogrid.provider",
						Interval: "30s",
						Rules: []Rule{
							{
								// Provider bandwidth spike: 5m rate > 100x rolling 1h baseline.
								// Per provider_id label so we know which to suspend.
								Alert: "ProviderBandwidthSpike",
								Expr: `(sum by (provider_id) (rate(providers_egress_bytes_total[5m])) ` +
									`> 1e6) and ` +
									`(sum by (provider_id) (rate(providers_egress_bytes_total[5m])) ` +
									`> 100 * sum by (provider_id) (rate(providers_egress_bytes_total[1h])))`,
								For: "2m",
								Labels: map[string]string{
									"severity": "page",
									"team":     "abuse",
									"action":   "temp_suspend",
								},
								Annotations: map[string]string{
									"summary":     "Provider {{ $labels.provider_id }} bandwidth >100x baseline",
									"description": "Provider {{ $labels.provider_id }} egress bandwidth over last 5m exceeds 100x the 1h rolling baseline — auto-suspend pending human review.",
									"runbook_url": "https://docs.iogrid.org/runbooks/provider-bandwidth-spike",
								},
							},
						},
					},
					{
						Name:     "iogrid.coordinator",
						Interval: "30s",
						Rules: []Rule{
							{
								// Service down: scrape failing for >5min.
								// We rely on the kube-prometheus default `up` metric
								// emitted by the prometheus receiver.
								Alert: "CoordinatorServiceDown",
								Expr:  `up{namespace="iogrid",job=~"iogrid-services"} == 0`,
								For:   "5m",
								Labels: map[string]string{
									"severity": "page",
									"team":     "platform",
								},
								Annotations: map[string]string{
									"summary":     "Coordinator service {{ $labels.service }} down >5m",
									"description": "Pod {{ $labels.pod }} in {{ $labels.namespace }} has been unscrapable for over 5 minutes.",
									"runbook_url": "https://docs.iogrid.org/runbooks/service-down",
								},
							},
						},
					},
				},
			},
		},
	}
}
