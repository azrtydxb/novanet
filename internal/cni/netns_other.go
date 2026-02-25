//go:build !linux

package cni

import (
	"net"
)

// setupPodNetwork is a no-op on non-Linux platforms. It returns a fake ifindex.
// This allows the CNI handler to be compiled and tested on macOS/Windows.
func setupPodNetwork(netnsPath, podIfName, hostVethName string, podIP, gateway net.IP, mac net.HardwareAddr, prefixLen int) (int, error) {
	// Return a fake ifindex for testing.
	return 42, nil
}

// cleanupPodNetwork is a no-op on non-Linux platforms.
func cleanupPodNetwork(netnsPath, hostVethName string) {
	// No-op.
}
