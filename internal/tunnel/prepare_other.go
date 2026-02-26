//go:build !linux

package tunnel

// PrepareOverlay is a no-op on non-Linux platforms.
func PrepareOverlay(protocol string) error {
	return nil
}
