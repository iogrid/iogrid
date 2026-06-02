package grid

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PromMetrics is the prometheus implementation of MetricsRecorder.
type PromMetrics struct {
	consumedTotal       prometheus.Counter
	providerPayoutTotal prometheus.Counter
	iogridCommission    prometheus.Counter
	settledTotal        *prometheus.CounterVec
}

// NewPromMetrics registers the counters with the default registry.
func NewPromMetrics() *PromMetrics {
	return &PromMetrics{
		consumedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "grid_consumed_total_atomic",
			Help: "Total $GRID consumed by VPN sessions, in atomic (9-decimal) units.",
		}),
		providerPayoutTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "grid_provider_payout_total_atomic",
			Help: "Total $GRID queued for provider payouts (85% of consumed).",
		}),
		iogridCommission: promauto.NewCounter(prometheus.CounterOpts{
			Name: "grid_iogrid_commission_total_atomic",
			Help: "Total $GRID accrued as iogrid commission (15% of consumed).",
		}),
		settledTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "grid_settlement_total",
			Help: "settlement-worker tick outcomes by status.",
		}, []string{"status"}),
	}
}

// RecordConsumed implements MetricsRecorder.
func (p *PromMetrics) RecordConsumed(a uint64) { p.consumedTotal.Add(float64(a)) }

// RecordProviderPayoutQueued implements MetricsRecorder.
func (p *PromMetrics) RecordProviderPayoutQueued(a uint64) { p.providerPayoutTotal.Add(float64(a)) }

// RecordIogridCommission implements MetricsRecorder.
func (p *PromMetrics) RecordIogridCommission(a uint64) { p.iogridCommission.Add(float64(a)) }

// SettledOK records a successful settlement-worker tick.
func (p *PromMetrics) SettledOK(n int) {
	p.settledTotal.WithLabelValues("ok").Add(float64(n))
}

// SettledFailed records a failed settlement-worker tick.
func (p *PromMetrics) SettledFailed(n int) {
	p.settledTotal.WithLabelValues("failed").Add(float64(n))
}
