//go:build !linux

package l2announce

import (
	"errors"
	"net"
)

// ErrNotSupported indicates that L2 announcements are only supported on Linux.
var ErrNotSupported = errors.New("L2 announcements require Linux (AF_PACKET)")

// sendGratuitousARP is a stub for non-Linux platforms.
func sendGratuitousARP(_ string, _ net.IP) error {
	return ErrNotSupported
}
