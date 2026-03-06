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

// AddRoute adds a kernel route for a CIDR via the tunnel interface.
// With collect-metadata (FlowBased) tunnels, the eBPF dataplane handles
// all encapsulation via bpf_skb_set_tunnel_key + bpf_redirect. The kernel
// route exists so that the RIB knows the CIDR is reachable through the
// tunnel device, which is needed for:
//   - ip route get queries
//   - host-originated traffic (kubelet health checks, DNS)
//   - FRR/BGP prefix visibility
//
// Uses RouteReplace to be idempotent.
func AddRoute(cidr string, ifName string, srcIP net.IP, remoteNodeIP net.IP, protocol string) error {
	_, dst, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parsing CIDR %s: %w", cidr, err)
	}

	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("finding interface %s: %w", ifName, err)
	}

	linkIdx := link.Attrs().Index

	// Use the dummy gateway with a neighbor entry so the kernel can
	// forward host-originated traffic through the tunnel device.
	// For pod-to-pod traffic, the eBPF dataplane bypasses this entirely.
	gatewayIP := dummyGateway

	neigh := &netlink.Neigh{
		LinkIndex:    linkIdx,
		IP:           gatewayIP,
		HardwareAddr: IPToTunnelMAC(remoteNodeIP),
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
	}
	if err := netlink.NeighSet(neigh); err != nil {
		return fmt.Errorf("adding neighbor entry on %s: %w", ifName, err)
	}

	route := &netlink.Route{
		Dst:       dst,
		Gw:        gatewayIP,
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

// AddBlackholeRoute installs a blackhole route for a CIDR. This makes the
// prefix visible in the kernel RIB so that FRR/BGP can advertise it via
// the "network" command. Individual /32 pod routes take precedence.
func AddBlackholeRoute(cidr string) error {
	_, dst, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parsing CIDR %s: %w", cidr, err)
	}

	route := &netlink.Route{
		Dst:  dst,
		Type: syscall.RTN_BLACKHOLE,
	}
	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("adding blackhole route %s: %w", cidr, err)
	}
	return nil
}

// RemoveBlackholeRoute removes a blackhole route for a CIDR.
func RemoveBlackholeRoute(cidr string) error {
	_, dst, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parsing CIDR %s: %w", cidr, err)
	}

	route := &netlink.Route{
		Dst:  dst,
		Type: syscall.RTN_BLACKHOLE,
	}
	if err := netlink.RouteDel(route); err != nil {
		return fmt.Errorf("removing blackhole route %s: %w", cidr, err)
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

// AddLoopbackAddress adds an IP address to the loopback interface.
// This is used to bind VIPs so the node can respond to traffic
// routed via BGP ECMP.
func AddLoopbackAddress(cidr string) error {
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("finding loopback interface: %w", err)
	}
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("parsing address %s: %w", cidr, err)
	}
	if err := netlink.AddrAdd(lo, addr); err != nil {
		return fmt.Errorf("adding %s to loopback: %w", cidr, err)
	}
	return nil
}

// RemoveLoopbackAddress removes an IP address from the loopback interface.
func RemoveLoopbackAddress(cidr string) error {
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("finding loopback interface: %w", err)
	}
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("parsing address %s: %w", cidr, err)
	}
	if err := netlink.AddrDel(lo, addr); err != nil {
		return fmt.Errorf("removing %s from loopback: %w", cidr, err)
	}
	return nil
}
