//go:build !linux

package tunnel

// destroyTunnel is a no-op on non-Linux platforms.
func destroyTunnel(ifName string) {
	// No-op.
}
