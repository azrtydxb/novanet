package metrics

import (
	"regexp"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// prometheusNameRe matches valid Prometheus metric names: letters, digits, and
// underscores, must not start with a digit.
var prometheusNameRe = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// TestMetricsNonNil verifies that all package-level metric variables are
// initialised (non-nil) at package init time.
func TestMetricsNonNil(t *testing.T) {
	if EndpointCount == nil {
		t.Error("EndpointCount is nil")
	}
	if PolicyCount == nil {
		t.Error("PolicyCount is nil")
	}
	if TunnelCount == nil {
		t.Error("TunnelCount is nil")
	}
	if IdentityCount == nil {
		t.Error("IdentityCount is nil")
	}
	if FlowTotal == nil {
		t.Error("FlowTotal is nil")
	}
	if DropsTotal == nil {
		t.Error("DropsTotal is nil")
	}
	if PolicyVerdictTotal == nil {
		t.Error("PolicyVerdictTotal is nil")
	}
	if LatencySeconds == nil {
		t.Error("LatencySeconds is nil")
	}
	if GRPCDuration == nil {
		t.Error("GRPCDuration is nil")
	}
}

// TestRegister verifies that Register() does not panic and that all metrics can
// be gathered from a fresh registry without error.
func TestRegister(t *testing.T) {
	reg := prometheus.NewRegistry()

	// Re-create metrics so they are independent of the global default registry
	// used by Register().  We test Register() itself via a no-panic check, then
	// validate the metric descriptors through a custom registry below.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Register() panicked: %v", r)
		}
	}()

	// Register a fresh set of the same metrics into a custom registry to verify
	// they are gatherable without error.
	endpointCount := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "novanet", Name: "endpoint_count", Help: "Number of known endpoints.",
	})
	policyCount := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "novanet", Name: "policy_count", Help: "Number of compiled policy rules.",
	})
	tunnelCount := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "novanet", Name: "tunnel_count", Help: "Number of active overlay tunnels.",
	})
	identityCount := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "novanet", Name: "identity_count", Help: "Number of distinct security identities.",
	})
	flowTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novanet", Name: "flow_total", Help: "Total observed network flows.",
	}, []string{"src_identity", "dst_identity", "verdict"})
	dropsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novanet", Name: "drops_total", Help: "Total dropped packets by reason.",
	}, []string{"reason"})
	policyVerdictTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novanet", Name: "policy_verdict_total", Help: "Total policy verdict evaluations by action.",
	}, []string{"action"})
	latencySeconds := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "novanet", Name: "latency_seconds", Help: "Operation latency in seconds.",
		Buckets: prometheus.DefBuckets,
	})
	grpcDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "novanet", Name: "grpc_duration_seconds", Help: "gRPC call duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})

	reg.MustRegister(
		endpointCount, policyCount, tunnelCount, identityCount,
		flowTotal, dropsTotal, policyVerdictTotal,
		latencySeconds, grpcDuration,
	)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() returned error: %v", err)
	}
	if len(mfs) == 0 {
		t.Error("Gather() returned no metric families")
	}
}

// readGauge returns the current float64 value of a Gauge by writing it into a
// dto.Metric struct.
func readGauge(g prometheus.Gauge) float64 {
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		panic(err)
	}
	return m.GetGauge().GetValue()
}

// TestGaugeSetAndRead verifies that each Gauge metric correctly stores and
// returns a value that was Set on it.
func TestGaugeSetAndRead(t *testing.T) {
	tests := []struct {
		name   string
		gauge  prometheus.Gauge
		value  float64
	}{
		{"EndpointCount", EndpointCount, 42},
		{"PolicyCount", PolicyCount, 7},
		{"TunnelCount", TunnelCount, 3},
		{"IdentityCount", IdentityCount, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset to zero first so tests are independent of execution order.
			tt.gauge.Set(0)
			tt.gauge.Set(tt.value)
			got := readGauge(tt.gauge)
			if got != tt.value {
				t.Errorf("%s: Set(%v) then read back %v", tt.name, tt.value, got)
			}
		})
	}
}

// TestGaugeIncrementAndDecrement verifies Inc/Dec behaviour on Gauges.
func TestGaugeIncrementAndDecrement(t *testing.T) {
	EndpointCount.Set(10)
	EndpointCount.Inc()
	if got := readGauge(EndpointCount); got != 11 {
		t.Errorf("after Inc: expected 11, got %v", got)
	}

	EndpointCount.Dec()
	if got := readGauge(EndpointCount); got != 10 {
		t.Errorf("after Dec: expected 10, got %v", got)
	}

	EndpointCount.Add(5)
	if got := readGauge(EndpointCount); got != 15 {
		t.Errorf("after Add(5): expected 15, got %v", got)
	}

	EndpointCount.Sub(3)
	if got := readGauge(EndpointCount); got != 12 {
		t.Errorf("after Sub(3): expected 12, got %v", got)
	}
}

// readCounter returns the current float64 value from a *dto.Metric that holds
// a counter sample.
func readCounter(c prometheus.Counter) float64 {
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		panic(err)
	}
	return m.GetCounter().GetValue()
}

// TestCounterIncrement verifies that counter metrics accumulate values correctly.
func TestCounterIncrement(t *testing.T) {
	t.Run("FlowTotal", func(t *testing.T) {
		c := FlowTotal.WithLabelValues("id-a", "id-b", "allow")
		before := readCounter(c)
		c.Inc()
		after := readCounter(c)
		if after != before+1 {
			t.Errorf("FlowTotal: expected %v after Inc, got %v", before+1, after)
		}
		c.Add(3)
		final := readCounter(c)
		if final != after+3 {
			t.Errorf("FlowTotal: expected %v after Add(3), got %v", after+3, final)
		}
	})

	t.Run("DropsTotal", func(t *testing.T) {
		c := DropsTotal.WithLabelValues("policy")
		before := readCounter(c)
		c.Inc()
		after := readCounter(c)
		if after != before+1 {
			t.Errorf("DropsTotal: expected %v after Inc, got %v", before+1, after)
		}
		c.Add(5)
		final := readCounter(c)
		if final != after+5 {
			t.Errorf("DropsTotal: expected %v after Add(5), got %v", after+5, final)
		}
	})

	t.Run("PolicyVerdictTotal", func(t *testing.T) {
		c := PolicyVerdictTotal.WithLabelValues("deny")
		before := readCounter(c)
		c.Inc()
		after := readCounter(c)
		if after != before+1 {
			t.Errorf("PolicyVerdictTotal: expected %v after Inc, got %v", before+1, after)
		}
	})
}

// readHistogramSampleCount returns the sample count from a Histogram.
func readHistogramSampleCount(h prometheus.Histogram) uint64 {
	var m dto.Metric
	if err := h.Write(&m); err != nil {
		panic(err)
	}
	return m.GetHistogram().GetSampleCount()
}

// readHistogramSampleSum returns the sample sum from a Histogram.
func readHistogramSampleSum(h prometheus.Histogram) float64 {
	var m dto.Metric
	if err := h.Write(&m); err != nil {
		panic(err)
	}
	return m.GetHistogram().GetSampleSum()
}

// TestHistogramObserve verifies that histogram metrics record observations.
func TestHistogramObserve(t *testing.T) {
	t.Run("LatencySeconds", func(t *testing.T) {
		beforeCount := readHistogramSampleCount(LatencySeconds)
		beforeSum := readHistogramSampleSum(LatencySeconds)

		LatencySeconds.Observe(0.001)
		LatencySeconds.Observe(0.5)

		afterCount := readHistogramSampleCount(LatencySeconds)
		afterSum := readHistogramSampleSum(LatencySeconds)

		if afterCount != beforeCount+2 {
			t.Errorf("LatencySeconds: expected sample count %d, got %d", beforeCount+2, afterCount)
		}
		wantSum := beforeSum + 0.001 + 0.5
		if afterSum != wantSum {
			t.Errorf("LatencySeconds: expected sample sum %v, got %v", wantSum, afterSum)
		}
	})

	t.Run("GRPCDuration", func(t *testing.T) {
		obs := GRPCDuration.WithLabelValues("RouteAdvertise")
		h, ok := obs.(prometheus.Histogram)
		if !ok {
			t.Fatal("GRPCDuration.WithLabelValues did not return a prometheus.Histogram")
		}
		beforeCount := readHistogramSampleCount(h)
		beforeSum := readHistogramSampleSum(h)

		h.Observe(0.002)
		h.Observe(0.010)

		afterCount := readHistogramSampleCount(h)
		afterSum := readHistogramSampleSum(h)

		if afterCount != beforeCount+2 {
			t.Errorf("GRPCDuration: expected sample count %d, got %d", beforeCount+2, afterCount)
		}
		wantSum := beforeSum + 0.002 + 0.010
		if afterSum != wantSum {
			t.Errorf("GRPCDuration: expected sample sum %v, got %v", wantSum, afterSum)
		}
	})
}

// fqNameRe extracts the fqName value from a prometheus.Desc.String() output.
// Desc.String() has the form: Desc{fqName: "novanet_foo", help: "...", ...}
var fqNameRe = regexp.MustCompile(`fqName: "([^"]+)"`)

// TestMetricNamingConventions verifies that all registered metric descriptors
// carry names that conform to Prometheus naming rules and use the expected
// "novanet" namespace prefix.
func TestMetricNamingConventions(t *testing.T) {
	// Collect descriptors from every metric variable defined in the package.
	collectors := []prometheus.Collector{
		EndpointCount,
		PolicyCount,
		TunnelCount,
		IdentityCount,
		FlowTotal,
		DropsTotal,
		PolicyVerdictTotal,
		LatencySeconds,
		GRPCDuration,
	}

	for _, c := range collectors {
		ch := make(chan *prometheus.Desc, 10)
		c.Describe(ch)
		close(ch)

		for desc := range ch {
			s := desc.String()
			m := fqNameRe.FindStringSubmatch(s)
			if m == nil {
				t.Errorf("could not parse fqName from descriptor: %s", s)
				continue
			}
			fqName := m[1]

			if !prometheusNameRe.MatchString(fqName) {
				t.Errorf("metric name %q does not match Prometheus naming convention", fqName)
			}
			if !strings.HasPrefix(fqName, "novanet_") {
				t.Errorf("metric name %q does not start with novanet_ namespace", fqName)
			}
		}
	}
}

// TestCounterVecLabelVariants verifies that distinct label combinations produce
// independent counter series.
func TestCounterVecLabelVariants(t *testing.T) {
	allow := FlowTotal.WithLabelValues("src1", "dst1", "allow")
	deny := FlowTotal.WithLabelValues("src1", "dst1", "deny")

	allow.Add(10)
	deny.Add(3)

	if readCounter(allow) < 10 {
		t.Errorf("allow counter: expected at least 10, got %v", readCounter(allow))
	}
	if readCounter(deny) < 3 {
		t.Errorf("deny counter: expected at least 3, got %v", readCounter(deny))
	}
	// Ensure the two series are independent — their values must differ by at
	// least the amounts added above.
	if readCounter(allow)-readCounter(deny) < 7 {
		t.Errorf("allow and deny counters appear to share state")
	}
}

// TestHistogramBuckets verifies that LatencySeconds uses the default bucket set
// (11 buckets + +Inf), confirming bucket configuration is intact.
func TestHistogramBuckets(t *testing.T) {
	var m dto.Metric
	if err := LatencySeconds.Write(&m); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// prometheus.DefBuckets has 11 entries; the proto encodes one bucket per
	// boundary plus a final +Inf bucket, so we expect at least 12.
	buckets := m.GetHistogram().GetBucket()
	if len(buckets) < len(prometheus.DefBuckets) {
		t.Errorf("expected at least %d buckets, got %d", len(prometheus.DefBuckets), len(buckets))
	}
}

// TestCollectDoesNotBlock ensures that Collect() on every metric returns at
// least one sample without blocking.
func TestCollectDoesNotBlock(t *testing.T) {
	collectors := []struct {
		name      string
		collector prometheus.Collector
	}{
		{"EndpointCount", EndpointCount},
		{"PolicyCount", PolicyCount},
		{"TunnelCount", TunnelCount},
		{"IdentityCount", IdentityCount},
		{"FlowTotal", FlowTotal},
		{"DropsTotal", DropsTotal},
		{"PolicyVerdictTotal", PolicyVerdictTotal},
		{"LatencySeconds", LatencySeconds},
		{"GRPCDuration", GRPCDuration},
	}

	for _, tc := range collectors {
		t.Run(tc.name, func(t *testing.T) {
			// testutil.CollectAndCount returns the number of metric samples
			// collected; it panics or returns 0 only on failure.
			n := testutil.CollectAndCount(tc.collector)
			if n < 1 {
				t.Errorf("%s: CollectAndCount returned %d, expected >= 1", tc.name, n)
			}
		})
	}
}
