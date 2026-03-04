//go:build !linux

package tunnel

import (
	"net"
	"sync/atomic"
)

// fakeVxlanIfindex provides monotonically increasing fake ifindex values for testing.
// Initialized to 200 so the first Add(1) returns 201.
var fakeVxlanIfindex = func() *atomic.Int32 {
	v := &atomic.Int32{}
	v.Store(200)
	return v
}()

// createVxlanTunnel is a no-op on non-Linux platforms.
func createVxlanTunnel(_ string, _ uint32, _ net.IP) (int, error) {
	return int(fakeVxlanIfindex.Add(1)), nil
}

func addVxlanFDB(_ string, _ net.HardwareAddr, _ net.IP) error    { return nil }
func removeVxlanFDB(_ string, _ net.HardwareAddr, _ net.IP) error { return nil }
