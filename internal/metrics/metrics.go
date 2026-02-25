// Package metrics defines and registers Prometheus metrics for NovaNet.
// It follows the same patterns as NovaRoute's metrics package.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// EndpointCount tracks the number of known endpoints.
	EndpointCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "novanet",
		Name:      "endpoint_count",
		Help:      "Number of known endpoints.",
	})

	// PolicyCount tracks the number of compiled policy rules.
	PolicyCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "novanet",
		Name:      "policy_count",
		Help:      "Number of compiled policy rules.",
	})

	// TunnelCount tracks the number of active tunnels.
	TunnelCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "novanet",
		Name:      "tunnel_count",
		Help:      "Number of active overlay tunnels.",
	})

	// IdentityCount tracks the number of distinct security identities.
	IdentityCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "novanet",
		Name:      "identity_count",
		Help:      "Number of distinct security identities.",
	})

	// FlowTotal counts observed network flows by source identity, destination
	// identity, and verdict.
	FlowTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novanet",
		Name:      "flow_total",
		Help:      "Total observed network flows.",
	}, []string{"src_identity", "dst_identity", "verdict"})

	// DropsTotal counts dropped packets by reason.
	DropsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novanet",
		Name:      "drops_total",
		Help:      "Total dropped packets by reason.",
	}, []string{"reason"})

	// PolicyVerdictTotal counts policy evaluation results by action.
	PolicyVerdictTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novanet",
		Name:      "policy_verdict_total",
		Help:      "Total policy verdict evaluations by action.",
	}, []string{"action"})

	// LatencySeconds observes general operation latencies.
	LatencySeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "novanet",
		Name:      "latency_seconds",
		Help:      "Operation latency in seconds.",
		Buckets:   prometheus.DefBuckets,
	})

	// GRPCDuration observes gRPC call durations by method.
	GRPCDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "novanet",
		Name:      "grpc_duration_seconds",
		Help:      "gRPC call duration in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method"})
)

// Register registers all NovaNet metrics with the default Prometheus registerer.
func Register() {
	prometheus.MustRegister(
		EndpointCount,
		PolicyCount,
		TunnelCount,
		IdentityCount,
		FlowTotal,
		DropsTotal,
		PolicyVerdictTotal,
		LatencySeconds,
		GRPCDuration,
	)
}
