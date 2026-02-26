//go:build !linux

package tunnel

import (
	"net"
	"sync/atomic"
)

var fakeVxlanIfindex atomic.Int32

func init() {
	fakeVxlanIfindex.Store(200)
}

// createVxlanTunnel is a no-op on non-Linux platforms.
// Returns a fake ifindex for testing.
func createVxlanTunnel(_, _ string, _ uint32, _ net.IP) (int, error) {
	return int(fakeVxlanIfindex.Add(1)), nil
}
