package agent

import (
	"github.com/azrtydxb/novanet/internal/agentmetrics"
	routingmetrics "github.com/azrtydxb/novanet/internal/routing/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricNamespace = "novanet"
	metricSubsystem = "agent"
)

// Prometheus metrics for the agent.
var (
	MetricEndpoints = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "endpoints_total",
		Help:      "Number of pod endpoints managed by the agent.",
	})
	MetricPolicies = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "policies_total",
		Help:      "Number of compiled policy rules.",
	})
	MetricTunnels = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "tunnels_total",
		Help:      "Number of overlay tunnels.",
	})
	MetricIdentities = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "identities_total",
		Help:      "Number of distinct identities.",
	})
	MetricCNIAddLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: "cni",
		Name:      "add_duration_seconds",
		Help:      "Latency of CNI ADD operations.",
		Buckets:   prometheus.DefBuckets,
	})
	MetricCNIDelLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Subsystem: "cni",
		Name:      "del_duration_seconds",
		Help:      "Latency of CNI DEL operations.",
		Buckets:   prometheus.DefBuckets,
	})
	MetricRemoteEndpoints = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "remote_endpoints_total",
		Help:      "Number of remote pod endpoints synced for cross-node identity resolution.",
	})
)

// RegisterMetrics registers all Prometheus metrics for the agent.
func RegisterMetrics() {
	prometheus.MustRegister(
		MetricEndpoints,
		MetricPolicies,
		MetricTunnels,
		MetricIdentities,
		MetricCNIAddLatency,
		MetricCNIDelLatency,
		MetricRemoteEndpoints,
	)
	// Register shared dataplane metrics (flow counters, TCP latency histogram, etc.).
	agentmetrics.Register()
	// Register routing subsystem metrics (FRR transactions, BGP peers, etc.).
	routingmetrics.Register()
}
