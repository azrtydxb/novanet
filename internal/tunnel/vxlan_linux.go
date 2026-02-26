//go:build linux

package tunnel

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// createVxlanTunnel creates a VXLAN tunnel interface on Linux.
// The interface is assigned a MAC derived from localIP so that decapsulated
// inner packets (whose destination MAC is set by the sending node's neighbor
// entry) are classified as PACKET_HOST by the kernel.
func createVxlanTunnel(name, remoteIP string, vni uint32, localIP net.IP) (int, error) {
	remote := net.ParseIP(remoteIP)
	if remote == nil {
		return 0, fmt.Errorf("invalid remote IP: %s", remoteIP)
	}

	vxlan := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:         name,
			HardwareAddr: IPToTunnelMAC(localIP),
		},
		VxlanId: int(vni),
		Group:   remote,
		Port:    4789, // Standard VXLAN port.
	}

	// Delete any stale interface from a previous run.
	if existing, err := netlink.LinkByName(name); err == nil {
		netlink.LinkDel(existing)
	}

	if err := netlink.LinkAdd(vxlan); err != nil {
		return 0, fmt.Errorf("creating vxlan interface %s: %w", name, err)
	}

	if err := netlink.LinkSetUp(vxlan); err != nil {
		netlink.LinkDel(vxlan)
		return 0, fmt.Errorf("bringing up vxlan interface %s: %w", name, err)
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return 0, fmt.Errorf("looking up vxlan interface %s: %w", name, err)
	}

	return link.Attrs().Index, nil
}
