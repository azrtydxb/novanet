//go:build linux

package tunnel

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// createGeneveTunnel creates a Geneve tunnel interface on Linux.
func createGeneveTunnel(name, remoteIP string, vni uint32) (int, error) {
	remote := net.ParseIP(remoteIP)
	if remote == nil {
		return 0, fmt.Errorf("invalid remote IP: %s", remoteIP)
	}

	geneve := &netlink.Geneve{
		LinkAttrs: netlink.LinkAttrs{
			Name: name,
		},
		ID:       vni,
		Remote:   remote,
		Dport:   6081, // Standard Geneve port.
	}

	if err := netlink.LinkAdd(geneve); err != nil {
		return 0, fmt.Errorf("creating geneve interface %s: %w", name, err)
	}

	if err := netlink.LinkSetUp(geneve); err != nil {
		netlink.LinkDel(geneve)
		return 0, fmt.Errorf("bringing up geneve interface %s: %w", name, err)
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return 0, fmt.Errorf("looking up geneve interface %s: %w", name, err)
	}

	return link.Attrs().Index, nil
}
