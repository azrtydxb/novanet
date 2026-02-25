//go:build linux

package cni

import (
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// setupPodNetwork creates a veth pair, moves one end into the pod's network
// namespace, and configures IP addressing.
// Returns the ifindex of the host-side veth.
func setupPodNetwork(netnsPath, podIfName, hostVethName string, podIP, gateway net.IP, mac net.HardwareAddr, prefixLen int) (int, error) {
	// Create the veth pair.
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  hostVethName,
			Flags: net.FlagUp,
		},
		PeerName: podIfName,
	}

	if err := netlink.LinkAdd(veth); err != nil {
		return 0, fmt.Errorf("creating veth pair: %w", err)
	}

	// Get the peer (pod side) interface.
	peerLink, err := netlink.LinkByName(podIfName)
	if err != nil {
		netlink.LinkDel(veth)
		return 0, fmt.Errorf("finding peer veth %s: %w", podIfName, err)
	}

	// Set MAC on the pod interface.
	if err := netlink.LinkSetHardwareAddr(peerLink, mac); err != nil {
		netlink.LinkDel(veth)
		return 0, fmt.Errorf("setting MAC on %s: %w", podIfName, err)
	}

	// Open the network namespace.
	nsfd, err := os.Open(netnsPath)
	if err != nil {
		netlink.LinkDel(veth)
		return 0, fmt.Errorf("opening netns %s: %w", netnsPath, err)
	}
	defer nsfd.Close()

	// Move the pod-side veth into the network namespace.
	if err := netlink.LinkSetNsFd(peerLink, int(nsfd.Fd())); err != nil {
		netlink.LinkDel(veth)
		return 0, fmt.Errorf("moving %s to netns: %w", podIfName, err)
	}

	// Configure the pod-side interface inside the namespace.
	if err := configureInNetns(nsfd, podIfName, podIP, gateway, prefixLen); err != nil {
		netlink.LinkDel(veth)
		return 0, fmt.Errorf("configuring pod interface: %w", err)
	}

	// Bring up the host-side veth.
	hostLink, err := netlink.LinkByName(hostVethName)
	if err != nil {
		return 0, fmt.Errorf("finding host veth %s: %w", hostVethName, err)
	}

	if err := netlink.LinkSetUp(hostLink); err != nil {
		return 0, fmt.Errorf("bringing up host veth: %w", err)
	}

	return hostLink.Attrs().Index, nil
}

// configureInNetns enters the network namespace and configures the pod interface.
func configureInNetns(nsfd *os.File, ifName string, podIP, gateway net.IP, prefixLen int) error {
	// Lock the goroutine to this OS thread so namespace changes are contained.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save the current namespace.
	curNS, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return fmt.Errorf("opening current netns: %w", err)
	}
	defer curNS.Close()

	// Enter the pod's namespace.
	if err := unix.Setns(int(nsfd.Fd()), unix.CLONE_NEWNET); err != nil {
		return fmt.Errorf("entering netns: %w", err)
	}
	defer unix.Setns(int(curNS.Fd()), unix.CLONE_NEWNET)

	// Find the interface.
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("finding interface %s in netns: %w", ifName, err)
	}

	// Add the IP address.
	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   podIP,
			Mask: net.CIDRMask(prefixLen, 32),
		},
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("adding address to %s: %w", ifName, err)
	}

	// Bring up the interface.
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bringing up %s: %w", ifName, err)
	}

	// Bring up loopback.
	lo, err := netlink.LinkByName("lo")
	if err == nil {
		netlink.LinkSetUp(lo)
	}

	// Add default route via the gateway.
	route := &netlink.Route{
		Dst: nil, // Default route.
		Gw:  gateway,
	}
	if err := netlink.RouteAdd(route); err != nil {
		return fmt.Errorf("adding default route: %w", err)
	}

	return nil
}

// cleanupPodNetwork removes the host-side veth (which also removes the pod side).
func cleanupPodNetwork(netnsPath, hostVethName string) {
	link, err := netlink.LinkByName(hostVethName)
	if err != nil {
		return
	}
	netlink.LinkDel(link)
}
