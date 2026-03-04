//go:build !linux

// Package cni provides CNI network setup and teardown operations.
package cni

import (
	"net"
)

// SetupPodNetwork is a no-op on non-Linux platforms. It returns a fake ifindex.
func SetupPodNetwork(netnsPath, podIfName, hostVethName string, podIP, gateway net.IP, mac net.HardwareAddr, prefixLen int) (int, error) {
	return 42, nil
}

// CleanupPodNetwork is a no-op on non-Linux platforms.
func CleanupPodNetwork(hostVethName string, podIP net.IP) {
}
