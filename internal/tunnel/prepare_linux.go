//go:build linux

package tunnel

import (
	"fmt"
	"os/exec"

	"github.com/vishvananda/netlink"
)

// PrepareOverlay cleans up stale tunnel interfaces and reloads the kernel
// module for the given protocol. This works around a kernel bug where
// the geneve module's internal hash table gets corrupted after repeated
// interface create/delete cycles, causing decapsulated inner packets to
// not be delivered to the IP stack.
//
// Must be called once at agent startup, before creating any tunnels.
func PrepareOverlay(protocol string) error {
	moduleName := protocol // "geneve" or "vxlan"

	// Delete all existing interfaces for this tunnel type.
	links, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("listing links: %w", err)
	}

	for _, link := range links {
		switch protocol {
		case "geneve":
			if _, ok := link.(*netlink.Geneve); ok {
				netlink.LinkDel(link)
			}
		case "vxlan":
			if _, ok := link.(*netlink.Vxlan); ok {
				netlink.LinkDel(link)
			}
		}
	}

	// Reload the kernel module to clear internal state.
	// Ignore errors — the module might not be loaded or might be builtin.
	exec.Command("modprobe", "-r", moduleName).Run()
	exec.Command("modprobe", moduleName).Run()

	return nil
}
