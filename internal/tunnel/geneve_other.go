//go:build !linux

package tunnel

import (
	"net"
	"sync/atomic"
)

var fakeGenevIfindex atomic.Int32

func init() {
	fakeGenevIfindex.Store(100)
}

// createGeneveTunnel is a no-op on non-Linux platforms.
// Returns a fake ifindex for testing.
func createGeneveTunnel(_, _ string, _ uint32, _ net.IP) (int, error) {
	return int(fakeGenevIfindex.Add(1)), nil
}
