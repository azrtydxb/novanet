//go:build linux

package tunnel

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// createVxlanTunnel creates or returns the single shared VXLAN interface
// in collect-metadata (FlowBased) mode. The eBPF dataplane uses
// bpf_skb_set_tunnel_key to set per-packet encap parameters before
// redirecting to this interface.
// If the interface already exists, its ifindex is returned without recreating it.
func createVxlanTunnel(name string, vni uint32, localIP net.IP) (int, error) {
	// Return existing interface if already created.
	if existing, err := netlink.LinkByName(name); err == nil {
		return existing.Attrs().Index, nil
	}

	vxlan := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:         name,
			HardwareAddr: IPToTunnelMAC(localIP),
		},
		VxlanId:   int(vni),
		Port:      4789,  // Standard VXLAN port.
		Learning:  false, // We manage encap via eBPF.
		FlowBased: true,  // Collect-metadata mode.
	}

	if err := netlink.LinkAdd(vxlan); err != nil {
		return 0, fmt.Errorf("creating vxlan interface %s: %w", name, err)
	}

	if err := netlink.LinkSetUp(vxlan); err != nil {
		_ = netlink.LinkDel(vxlan)
		return 0, fmt.Errorf("bringing up vxlan interface %s: %w", name, err)
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return 0, fmt.Errorf("looking up vxlan interface %s: %w", name, err)
	}

	return link.Attrs().Index, nil
}
