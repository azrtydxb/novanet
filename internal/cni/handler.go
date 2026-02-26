// Package cni implements the CNI handler for ADD/DEL pod network requests.
package cni

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"

	"go.uber.org/zap"

	"github.com/piwi3910/novanet/internal/identity"
	"github.com/piwi3910/novanet/internal/ipam"
)

// DataplaneClient is the interface for interacting with the eBPF dataplane.
type DataplaneClient interface {
	// UpsertEndpoint registers or updates an endpoint in the dataplane.
	UpsertEndpoint(ctx context.Context, ip uint32, ifindex int, mac net.HardwareAddr, identityID uint32, podName, namespace string, nodeIP uint32) error
	// DeleteEndpoint removes an endpoint from the dataplane.
	DeleteEndpoint(ctx context.Context, ip uint32) error
	// AttachProgram attaches a TC program to an interface.
	AttachProgram(ctx context.Context, iface string, ingress bool) error
}

// AddPodRequest contains the parameters for adding a pod to the network.
type AddPodRequest struct {
	PodName      string
	PodNamespace string
	ContainerID  string
	Netns        string
	IfName       string
	Labels       map[string]string
}

// AddPodResult contains the results of a pod network setup.
type AddPodResult struct {
	IP           net.IP
	Gateway      net.IP
	Mac          net.HardwareAddr
	PrefixLength int
}

// DelPodRequest contains the parameters for removing a pod from the network.
type DelPodRequest struct {
	PodName      string
	PodNamespace string
	ContainerID  string
	Netns        string
	IfName       string
}

// Handler processes CNI ADD/DEL requests.
type Handler struct {
	ipam         *ipam.Allocator
	identityAlloc *identity.Allocator
	dpClient     DataplaneClient
	logger       *zap.Logger

	// nodeIP is the IP address of this node (as uint32 for the dataplane).
	nodeIP uint32

	// endpoints tracks allocated IPs for cleanup on DEL.
	// Key is containerID, value is the allocated IP.
	endpoints map[string]endpointInfo
}

type endpointInfo struct {
	ip         net.IP
	ipUint32   uint32
	identityID uint32
	hostVeth   string
}

// NewHandler creates a new CNI handler.
func NewHandler(ipam *ipam.Allocator, identityAlloc *identity.Allocator, dpClient DataplaneClient, logger *zap.Logger) *Handler {
	return &Handler{
		ipam:         ipam,
		identityAlloc: identityAlloc,
		dpClient:     dpClient,
		logger:       logger,
		endpoints:    make(map[string]endpointInfo),
	}
}

// SetNodeIP sets the node IP (as uint32, network byte order).
func (h *Handler) SetNodeIP(nodeIP uint32) {
	h.nodeIP = nodeIP
}

// Add handles a CNI ADD request: allocates an IP, creates a veth pair,
// configures the pod network namespace, and registers the endpoint with
// the dataplane.
func (h *Handler) Add(req *AddPodRequest) (*AddPodResult, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}

	h.logger.Info("CNI ADD",
		zap.String("pod", req.PodNamespace+"/"+req.PodName),
		zap.String("container_id", req.ContainerID),
		zap.String("netns", req.Netns),
	)

	// Allocate an IP address.
	podIP, err := h.ipam.Allocate()
	if err != nil {
		return nil, fmt.Errorf("allocating IP: %w", err)
	}

	// Compute identity from labels.
	identityID := h.identityAlloc.AllocateIdentity(req.Labels)

	// Generate a random MAC address for the pod interface.
	mac, err := generateMAC()
	if err != nil {
		h.ipam.Release(podIP)
		return nil, fmt.Errorf("generating MAC address: %w", err)
	}

	gateway := h.ipam.Gateway()
	prefixLen := h.ipam.PrefixLength()

	// Create veth pair and configure the pod network namespace.
	hostVethName := vethNameForPod(req.PodName, req.PodNamespace)
	ifindex, err := SetupPodNetwork(req.Netns, req.IfName, hostVethName, podIP, gateway, mac, prefixLen)
	if err != nil {
		h.ipam.Release(podIP)
		return nil, fmt.Errorf("setting up pod network: %w", err)
	}

	ipUint32 := ipToUint32(podIP)

	// Register with the dataplane.
	ctx := context.Background()
	err = h.dpClient.UpsertEndpoint(ctx, ipUint32, ifindex, mac, identityID, req.PodName, req.PodNamespace, h.nodeIP)
	if err != nil {
		CleanupPodNetwork(hostVethName, podIP)
		h.ipam.Release(podIP)
		return nil, fmt.Errorf("registering endpoint with dataplane: %w", err)
	}

	// Attach TC programs to the host veth.
	err = h.dpClient.AttachProgram(ctx, hostVethName, true)
	if err != nil {
		h.dpClient.DeleteEndpoint(ctx, ipUint32)
		CleanupPodNetwork(hostVethName, podIP)
		h.ipam.Release(podIP)
		return nil, fmt.Errorf("attaching TC ingress program: %w", err)
	}

	err = h.dpClient.AttachProgram(ctx, hostVethName, false)
	if err != nil {
		h.dpClient.DeleteEndpoint(ctx, ipUint32)
		CleanupPodNetwork(hostVethName, podIP)
		h.ipam.Release(podIP)
		return nil, fmt.Errorf("attaching TC egress program: %w", err)
	}

	// Track the endpoint for cleanup.
	h.endpoints[req.ContainerID] = endpointInfo{
		ip:         podIP,
		ipUint32:   ipUint32,
		identityID: identityID,
		hostVeth:   hostVethName,
	}

	h.logger.Info("CNI ADD complete",
		zap.String("pod", req.PodNamespace+"/"+req.PodName),
		zap.String("ip", podIP.String()),
		zap.Uint32("identity", identityID),
	)

	return &AddPodResult{
		IP:           podIP,
		Gateway:      gateway,
		Mac:          mac,
		PrefixLength: prefixLen,
	}, nil
}

// Del handles a CNI DEL request: releases the IP, removes the endpoint,
// and cleans up the veth pair.
func (h *Handler) Del(req *DelPodRequest) error {
	if req == nil {
		return fmt.Errorf("nil request")
	}

	h.logger.Info("CNI DEL",
		zap.String("pod", req.PodNamespace+"/"+req.PodName),
		zap.String("container_id", req.ContainerID),
	)

	ep, ok := h.endpoints[req.ContainerID]
	if !ok {
		h.logger.Warn("endpoint not found for container, skipping cleanup",
			zap.String("container_id", req.ContainerID),
		)
		return nil
	}

	ctx := context.Background()

	// Delete the endpoint from the dataplane.
	if err := h.dpClient.DeleteEndpoint(ctx, ep.ipUint32); err != nil {
		h.logger.Error("failed to delete endpoint from dataplane",
			zap.Error(err),
			zap.String("container_id", req.ContainerID),
		)
	}

	// Clean up the veth pair and host route.
	CleanupPodNetwork(ep.hostVeth, ep.ip)

	// Release the IP address.
	if err := h.ipam.Release(ep.ip); err != nil {
		h.logger.Error("failed to release IP",
			zap.Error(err),
			zap.String("ip", ep.ip.String()),
		)
	}

	// Remove identity reference.
	h.identityAlloc.RemoveIdentity(ep.identityID)

	delete(h.endpoints, req.ContainerID)

	h.logger.Info("CNI DEL complete",
		zap.String("pod", req.PodNamespace+"/"+req.PodName),
	)

	return nil
}

// generateMAC generates a random MAC address with the locally-administered bit set.
func generateMAC() (net.HardwareAddr, error) {
	mac := make([]byte, 6)
	_, err := rand.Read(mac)
	if err != nil {
		return nil, fmt.Errorf("reading random bytes: %w", err)
	}
	// Set locally administered and unicast bits.
	mac[0] = (mac[0] | 0x02) & 0xfe
	return net.HardwareAddr(mac), nil
}

// vethNameForPod generates a deterministic host veth name for a pod.
// Truncated to 15 chars to fit the Linux interface name limit.
func vethNameForPod(name, namespace string) string {
	full := "nv_" + namespace + "_" + name
	if len(full) > 15 {
		full = full[:15]
	}
	return full
}

// ipToUint32 converts an IPv4 address to a uint32 in network byte order.
func ipToUint32(ip net.IP) uint32 {
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	return uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
}
