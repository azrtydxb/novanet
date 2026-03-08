//go:build linux

package l2announce

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

var (
	// errIPv6NANotImplemented is returned when IPv6 neighbor advertisement is requested.
	errIPv6NANotImplemented = errors.New("IPv6 neighbor advertisement not yet implemented")
	// errInvalidHWAddrLen is returned when an interface has a hardware address shorter than 6 bytes.
	errInvalidHWAddrLen = errors.New("interface has invalid hardware address length")
)

// sendGratuitousARP sends a Gratuitous ARP (ARP reply) packet on the
// specified interface to announce ownership of the given IPv4 address.
// For IPv6 addresses, an Unsolicited Neighbor Advertisement is sent instead.
func sendGratuitousARP(iface string, ip net.IP) error {
	ip4 := ip.To4()
	if ip4 == nil {
		return sendUnsolicitedNA(iface, ip)
	}

	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return fmt.Errorf("interface lookup %s: %w", iface, err)
	}

	if len(ifi.HardwareAddr) < 6 {
		return fmt.Errorf("%w: %s (got %d bytes)", errInvalidHWAddrLen, iface, len(ifi.HardwareAddr))
	}
	hwAddr := ifi.HardwareAddr[:6] //nolint:gosec // G602: length checked above

	// Open raw AF_PACKET socket for sending Ethernet frames.
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(syscall.ETH_P_ARP)))
	if err != nil {
		return fmt.Errorf("open raw socket: %w", err)
	}
	defer func() { _ = syscall.Close(fd) }()

	// Build the GARP packet.
	// Ethernet header (14 bytes) + ARP payload (28 bytes) = 42 bytes.
	pkt := make([]byte, 42)

	// Ethernet header.
	// Destination: broadcast ff:ff:ff:ff:ff:ff
	for i := 0; i < 6; i++ {
		pkt[i] = 0xff
	}
	// Source: interface MAC.
	copy(pkt[6:12], hwAddr)
	// EtherType: ARP (0x0806).
	binary.BigEndian.PutUint16(pkt[12:14], syscall.ETH_P_ARP)

	// ARP payload.
	arpOffset := 14
	binary.BigEndian.PutUint16(pkt[arpOffset:], 1)        // Hardware type: Ethernet
	binary.BigEndian.PutUint16(pkt[arpOffset+2:], 0x0800) // Protocol type: IPv4
	pkt[arpOffset+4] = 6                                  // Hardware address length
	pkt[arpOffset+5] = 4                                  // Protocol address length
	binary.BigEndian.PutUint16(pkt[arpOffset+6:], 2)      // Operation: ARP reply (GARP)

	// Sender hardware address.
	copy(pkt[arpOffset+8:arpOffset+14], hwAddr)
	// Sender protocol address.
	copy(pkt[arpOffset+14:arpOffset+18], ip4)
	// Target hardware address: broadcast (bytes 32-37 of 42-byte packet).
	for i := arpOffset + 18; i < arpOffset+24; i++ {
		pkt[i] = 0xff //nolint:gosec // G602: pkt is 42 bytes, max index is arpOffset+23=37
	}
	// Target protocol address: same as sender (gratuitous).
	copy(pkt[arpOffset+24:arpOffset+28], ip4)

	// Send the packet.
	addr := syscall.SockaddrLinklayer{
		Protocol: htons(syscall.ETH_P_ARP),
		Ifindex:  ifi.Index,
	}
	return syscall.Sendto(fd, pkt, 0, &addr)
}

// sendUnsolicitedNA sends an ICMPv6 Neighbor Advertisement for IPv6 addresses.
func sendUnsolicitedNA(_ string, ip net.IP) error {
	// IPv6 NA implementation is a future enhancement.
	return fmt.Errorf("%w for %s", errIPv6NANotImplemented, ip)
}

// htons converts a uint16 from host to network byte order.
func htons(v uint16) uint16 {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	//nolint:gosec // G103: unsafe.Pointer is required for byte-order conversion; b is a local 2-byte slice.
	return *(*uint16)(unsafe.Pointer(&b[0]))
}
