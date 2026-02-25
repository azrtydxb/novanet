//go:build linux

package tunnel

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// createVxlanTunnel creates a VXLAN tunnel interface on Linux.
func createVxlanTunnel(name, remoteIP string, vni uint32) (int, error) {
	remote := net.ParseIP(remoteIP)
	if remote == nil {
		return 0, fmt.Errorf("invalid remote IP: %s", remoteIP)
	}

	vxlan := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name: name,
		},
		VxlanId: int(vni),
		Group:   remote,
		Port:    4789, // Standard VXLAN port.
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
