//go:build !linux

package tunnel

import (
	"net"
	"sync/atomic"
)

// fakeGenevIfindex provides monotonically increasing fake ifindex values for testing.
// Initialized to 100 so the first Add(1) returns 101.
var fakeGenevIfindex = func() *atomic.Int32 {
	v := &atomic.Int32{}
	v.Store(100)
	return v
}()

// createGeneveTunnel is a no-op on non-Linux platforms.
// Returns a fake ifindex for testing.
func createGeneveTunnel(_, _ string, _ uint32, _ net.IP) (int, error) {
	return int(fakeGenevIfindex.Add(1)), nil
}
