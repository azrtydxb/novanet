//go:build !linux

package masquerade

import "fmt"

// EnsureMasquerade is not supported on non-Linux platforms.
func EnsureMasquerade(podCIDR, clusterCIDR string) error {
	return fmt.Errorf("masquerade not supported on this platform")
}
