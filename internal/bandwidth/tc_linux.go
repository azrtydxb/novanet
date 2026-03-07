//go:build linux

package bandwidth

import (
	"fmt"
	"os/exec"
)

// applyTCQdisc applies a TBF (Token Bucket Filter) qdisc on the given interface.
// rateBPS is the rate in bytes per second.
func applyTCQdisc(ifaceName string, rateBPS uint64) error {
	// Remove any existing qdisc first (ignore errors if none exists).
	_ = removeTCQdisc(ifaceName)

	// Calculate burst size: at least 1 MTU (1600 bytes) or rate/250 (Hz), whichever is larger.
	// This ensures the bucket can hold at least one packet.
	burst := rateBPS / 250
	if burst < 1600 {
		burst = 1600
	}

	// tc rate is in bytes/sec when using "bps" suffix.
	rateStr := fmt.Sprintf("%dbps", rateBPS)
	burstStr := fmt.Sprintf("%d", burst)

	//nolint:gosec // Arguments are constructed from validated uint64 values, not user input.
	cmd := exec.Command("tc", "qdisc", "add", "dev", ifaceName, "root", "tbf",
		"rate", rateStr,
		"burst", burstStr,
		"latency", "50ms",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tc qdisc add failed on %s: %w (output: %s)", ifaceName, err, string(output))
	}

	return nil
}

// removeTCQdisc removes TC qdisc from the given interface.
func removeTCQdisc(ifaceName string) error {
	//nolint:gosec // Arguments are constructed from validated string values, not user input.
	cmd := exec.Command("tc", "qdisc", "del", "dev", ifaceName, "root")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tc qdisc del failed on %s: %w (output: %s)", ifaceName, err, string(output))
	}

	return nil
}
