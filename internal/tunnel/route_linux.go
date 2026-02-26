//go:build linux

package tunnel

import (
	"fmt"
	"net"
	"syscall"

	"github.com/vishvananda/netlink"
)

// dummyGateway is a link-local IP used as a dummy next-hop on Geneve interfaces.
// Since Geneve tunnels are point-to-point by nature (each has a fixed remote),
// we route through this dummy gateway with a permanent neighbor entry to avoid
// ARP resolution issues.
var dummyGateway = net.ParseIP("169.254.1.1")

// IPToTunnelMAC derives a deterministic locally-administered unicast MAC
// address from an IPv4 address. Each node uses its own IP to derive the MAC
// for its tunnel interfaces. The neighbor entry on the sending side uses
// the remote node's derived MAC so that the inner Ethernet frame's
// destination MAC matches the receiving tunnel interface's MAC, preventing
// the kernel from classifying the packet as PACKET_OTHERHOST (dropped in
// ip_rcv) or PACKET_LOOPBACK (when src == dst == interface MAC).
func IPToTunnelMAC(ip net.IP) net.HardwareAddr {
	ip4 := ip.To4()
	if ip4 == nil {
		return net.HardwareAddr{0xaa, 0xbb, 0, 0, 0, 0}
	}
	return net.HardwareAddr{0xaa, 0xbb, ip4[0], ip4[1], ip4[2], ip4[3]}
}

// AddRoute adds a kernel route for a CIDR via a tunnel interface.
// srcIP is set as the preferred source on the route so that the kernel
// uses the node's real IP instead of any VIP on loopback.
// remoteNodeIP is the remote node's IP, used to derive the neighbor entry's
// MAC address (must match the remote tunnel interface's MAC).
// It sets up a permanent neighbor entry for the dummy gateway and routes
// the CIDR via that gateway with the onlink flag. This avoids ARP resolution
// failures on the Geneve interface.
// Uses RouteReplace to be idempotent.
func AddRoute(cidr string, ifName string, srcIP net.IP, remoteNodeIP net.IP) error {
	_, dst, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parsing CIDR %s: %w", cidr, err)
	}

	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("finding interface %s: %w", ifName, err)
	}

	linkIdx := link.Attrs().Index

	// Add a permanent neighbor entry for the dummy gateway using the remote
	// node's derived MAC. This MAC must match the receiving tunnel interface's
	// MAC so that the kernel classifies decapsulated inner packets as
	// PACKET_HOST (not PACKET_OTHERHOST or PACKET_LOOPBACK).
	neigh := &netlink.Neigh{
		LinkIndex:    linkIdx,
		IP:           dummyGateway,
		HardwareAddr: IPToTunnelMAC(remoteNodeIP),
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
	}
	if err := netlink.NeighSet(neigh); err != nil {
		return fmt.Errorf("adding neighbor entry on %s: %w", ifName, err)
	}

	// Route the CIDR via the dummy gateway with the onlink flag.
	// The onlink flag tells the kernel the gateway is directly reachable on
	// this link even though it's not in a connected subnet.
	route := &netlink.Route{
		Dst:       dst,
		Gw:        dummyGateway,
		Src:       srcIP,
		LinkIndex: linkIdx,
		Scope:     netlink.SCOPE_UNIVERSE,
		Flags:     int(netlink.FLAG_ONLINK),
	}
	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("adding route %s via %s: %w", cidr, ifName, err)
	}

	return nil
}

// RemoveRoute removes a kernel route for a CIDR.
func RemoveRoute(cidr string) error {
	_, dst, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parsing CIDR %s: %w", cidr, err)
	}

	route := &netlink.Route{Dst: dst}
	if err := netlink.RouteDel(route); err != nil {
		return fmt.Errorf("removing route %s: %w", cidr, err)
	}

	return nil
}
