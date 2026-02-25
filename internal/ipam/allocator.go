// Package ipam provides a bitmap-based IP address management allocator
// for per-node Pod IP allocation within a given PodCIDR.
package ipam

import (
	"fmt"
	"math/big"
	"net"
	"sync"
)

// Allocator manages a pool of IP addresses within a single CIDR.
// It uses a bitmap stored as []uint64 for efficient allocation tracking.
type Allocator struct {
	mu sync.Mutex

	// network is the parsed CIDR network.
	network *net.IPNet
	// baseIP is the network address as a 4-byte IP.
	baseIP net.IP
	// size is the total number of IPs in the CIDR.
	size int
	// bitmap tracks which IPs are allocated. Bit i corresponds to baseIP + i.
	bitmap []uint64
	// used counts the number of currently allocated IPs (including reserved).
	used int
	// prefixLen is the CIDR prefix length.
	prefixLen int
}

// NewAllocator creates a new IPAM allocator for the given PodCIDR.
// The network address (.0) and gateway address (.1) are automatically reserved.
func NewAllocator(podCIDR string) (*Allocator, error) {
	ip, network, err := net.ParseCIDR(podCIDR)
	if err != nil {
		return nil, fmt.Errorf("parsing podCIDR %q: %w", podCIDR, err)
	}

	// Only support IPv4 for now.
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("only IPv4 is supported, got %q", podCIDR)
	}

	ones, bits := network.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("only IPv4 is supported, got %d-bit mask", bits)
	}

	size := 1 << (bits - ones)
	if size < 4 {
		return nil, fmt.Errorf("CIDR %q is too small (need at least /30)", podCIDR)
	}

	// Calculate number of uint64 words needed.
	words := (size + 63) / 64

	a := &Allocator{
		network:   network,
		baseIP:    network.IP.To4(),
		size:      size,
		bitmap:    make([]uint64, words),
		prefixLen: ones,
	}

	// Reserve .0 (network address) and .1 (gateway).
	a.setBit(0)
	a.setBit(1)
	a.used = 2

	// Reserve broadcast address for /24 and larger.
	if size > 2 {
		a.setBit(size - 1)
		a.used = 3
	}

	return a, nil
}

// Allocate returns the next available IP address from the pool.
func (a *Allocator) Allocate() (net.IP, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	idx := a.findFree()
	if idx < 0 {
		return nil, fmt.Errorf("no free IP addresses in %s", a.network.String())
	}

	a.setBit(idx)
	a.used++

	return a.indexToIP(idx), nil
}

// AllocateSpecific claims a specific IP address from the pool.
func (a *Allocator) AllocateSpecific(ip net.IP) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	ip4 := ip.To4()
	if ip4 == nil {
		return fmt.Errorf("only IPv4 is supported")
	}

	if !a.network.Contains(ip4) {
		return fmt.Errorf("IP %s is not within CIDR %s", ip.String(), a.network.String())
	}

	idx := a.ipToIndex(ip4)
	if idx < 0 || idx >= a.size {
		return fmt.Errorf("IP %s is out of range", ip.String())
	}

	if a.getBit(idx) {
		return fmt.Errorf("IP %s is already allocated", ip.String())
	}

	a.setBit(idx)
	a.used++

	return nil
}

// Release frees a previously allocated IP address.
func (a *Allocator) Release(ip net.IP) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	ip4 := ip.To4()
	if ip4 == nil {
		return fmt.Errorf("only IPv4 is supported")
	}

	if !a.network.Contains(ip4) {
		return fmt.Errorf("IP %s is not within CIDR %s", ip.String(), a.network.String())
	}

	idx := a.ipToIndex(ip4)
	if idx < 0 || idx >= a.size {
		return fmt.Errorf("IP %s is out of range", ip.String())
	}

	// Prevent releasing reserved addresses.
	if idx == 0 {
		return fmt.Errorf("cannot release network address %s", ip.String())
	}
	if idx == 1 {
		return fmt.Errorf("cannot release gateway address %s", ip.String())
	}
	if idx == a.size-1 {
		return fmt.Errorf("cannot release broadcast address %s", ip.String())
	}

	if !a.getBit(idx) {
		return fmt.Errorf("IP %s is not allocated", ip.String())
	}

	a.clearBit(idx)
	a.used--

	return nil
}

// Used returns the number of allocated IPs (including reserved addresses).
func (a *Allocator) Used() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.used
}

// Available returns the number of IPs available for allocation.
// This excludes the reserved .0, .1, and broadcast addresses.
func (a *Allocator) Available() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.size - a.used
}

// Gateway returns the gateway IP (.1 of the CIDR).
func (a *Allocator) Gateway() net.IP {
	return a.indexToIP(1)
}

// PrefixLength returns the CIDR prefix length.
func (a *Allocator) PrefixLength() int {
	return a.prefixLen
}

// CIDR returns the network CIDR string.
func (a *Allocator) CIDR() string {
	return a.network.String()
}

// findFree returns the index of the first unset bit, or -1 if all are set.
func (a *Allocator) findFree() int {
	for i, word := range a.bitmap {
		if word == ^uint64(0) {
			continue
		}
		// Find the first zero bit in this word.
		for bit := 0; bit < 64; bit++ {
			idx := i*64 + bit
			if idx >= a.size {
				return -1
			}
			if word&(1<<uint(bit)) == 0 {
				return idx
			}
		}
	}
	return -1
}

// setBit sets the bit at the given index.
func (a *Allocator) setBit(idx int) {
	word := idx / 64
	bit := uint(idx % 64)
	a.bitmap[word] |= 1 << bit
}

// clearBit clears the bit at the given index.
func (a *Allocator) clearBit(idx int) {
	word := idx / 64
	bit := uint(idx % 64)
	a.bitmap[word] &^= 1 << bit
}

// getBit returns true if the bit at the given index is set.
func (a *Allocator) getBit(idx int) bool {
	word := idx / 64
	bit := uint(idx % 64)
	return a.bitmap[word]&(1<<bit) != 0
}

// ipToIndex converts an IP address to a bitmap index.
func (a *Allocator) ipToIndex(ip net.IP) int {
	ip4 := ip.To4()
	base := big.NewInt(0).SetBytes(a.baseIP)
	addr := big.NewInt(0).SetBytes(ip4)
	offset := big.NewInt(0).Sub(addr, base)
	return int(offset.Int64())
}

// indexToIP converts a bitmap index to an IP address.
func (a *Allocator) indexToIP(idx int) net.IP {
	base := big.NewInt(0).SetBytes(a.baseIP)
	offset := big.NewInt(int64(idx))
	result := big.NewInt(0).Add(base, offset)

	b := result.Bytes()
	ip := make(net.IP, 4)
	// Pad with leading zeros.
	copy(ip[4-len(b):], b)
	return ip
}
