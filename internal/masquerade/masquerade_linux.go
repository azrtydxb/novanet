//go:build linux

package masquerade

import (
	"fmt"
	"os/exec"
	"strings"
)

// EnsureMasquerade adds an iptables MASQUERADE rule for pod traffic leaving
// the cluster. It is idempotent: if the rule already exists, it does nothing.
func EnsureMasquerade(podCIDR, clusterCIDR string) error {
	// Check if the rule already exists.
	checkArgs := []string{
		"-t", "nat", "-C", "POSTROUTING",
		"-s", podCIDR,
		"!", "-d", clusterCIDR,
		"-j", "MASQUERADE",
		"-m", "comment", "--comment", "novanet masquerade",
	}
	if err := exec.Command("iptables", checkArgs...).Run(); err == nil {
		return nil // Rule already exists.
	}

	// Add the rule.
	addArgs := []string{
		"-t", "nat", "-A", "POSTROUTING",
		"-s", podCIDR,
		"!", "-d", clusterCIDR,
		"-j", "MASQUERADE",
		"-m", "comment", "--comment", "novanet masquerade",
	}
	out, err := exec.Command("iptables", addArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adding masquerade rule: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}
