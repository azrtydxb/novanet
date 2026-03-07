// Package lbipam provides IP address management for Kubernetes Services
// of type LoadBalancer. It allocates IPs from configured address pools
// and tracks which service owns each allocation.
package lbipam

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"

	"go.uber.org/zap"
)

// Sentinel errors for the LB-IPAM allocator.
var (
	ErrNoPoolSpace     = errors.New("no pool has available addresses")
	ErrPoolNotFound    = errors.New("pool not found")
	ErrPoolExists      = errors.New("pool already exists")
	ErrInvalidCIDR     = errors.New("invalid CIDR notation")
	ErrIPAlreadyInUse  = errors.New("IP already allocated")
	ErrIPNotAllocated  = errors.New("IP is not allocated")
	ErrPoolExhausted   = errors.New("pool exhausted")
	ErrAlreadyAssigned = errors.New("service already has an allocation")
)

// Pool represents a named block of IP addresses available for allocation
// to LoadBalancer Services.
type Pool struct {
	Name string
	CIDR net.IPNet
	used map[string]string // IP string -> service key (namespace/name)
}

// Allocator manages multiple IP pools and allocates addresses to Services.
type Allocator struct {
	mu     sync.RWMutex
	pools  []*Pool
	logger *zap.Logger
}

// NewAllocator creates a new LB-IPAM allocator.
func NewAllocator(logger *zap.Logger) *Allocator {
	return &Allocator{
		pools:  make([]*Pool, 0),
		logger: logger,
	}
}

// AddPool registers a new IP pool with the given name and CIDR range.
func (a *Allocator) AddPool(name, cidr string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, p := range a.pools {
		if p.Name == name {
			return fmt.Errorf("%w: %s", ErrPoolExists, name)
		}
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidCIDR, err.Error())
	}

	a.pools = append(a.pools, &Pool{
		Name: name,
		CIDR: *ipNet,
		used: make(map[string]string),
	})

	a.logger.Info("added LB-IPAM pool", zap.String("name", name), zap.String("cidr", cidr))
	return nil
}

// RemovePool removes a pool by name, releasing all its allocations.
func (a *Allocator) RemovePool(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i, p := range a.pools {
		if p.Name == name {
			a.pools = append(a.pools[:i], a.pools[i+1:]...)
			a.logger.Info("removed LB-IPAM pool", zap.String("name", name))
			return
		}
	}
}

// Allocate assigns an IP from the first pool with available space.
func (a *Allocator) Allocate(serviceKey string) (net.IP, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, pool := range a.pools {
		ip, err := a.allocateFromPoolLocked(pool, serviceKey)
		if err == nil {
			return ip, nil
		}
		// If pool is exhausted, try next pool.
		if !errors.Is(err, ErrPoolExhausted) {
			return nil, err
		}
	}
	return nil, ErrNoPoolSpace
}

// AllocateFromPool assigns an IP from the specified pool.
func (a *Allocator) AllocateFromPool(poolName, serviceKey string) (net.IP, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, pool := range a.pools {
		if pool.Name == poolName {
			return a.allocateFromPoolLocked(pool, serviceKey)
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrPoolNotFound, poolName)
}

// allocateFromPoolLocked finds the next free IP in a pool and assigns it.
// Caller must hold a.mu.
func (a *Allocator) allocateFromPoolLocked(pool *Pool, serviceKey string) (net.IP, error) {
	size := poolSize(&pool.CIDR)
	if uint64(len(pool.used)) >= size {
		return nil, ErrPoolExhausted
	}

	ip := cloneIP(pool.CIDR.IP)
	for i := uint64(0); i < size; i++ {
		candidate := addToIP(ip, i)
		key := candidate.String()
		if _, taken := pool.used[key]; !taken {
			pool.used[key] = serviceKey
			a.logger.Info("allocated LB IP",
				zap.String("ip", key),
				zap.String("pool", pool.Name),
				zap.String("service", serviceKey),
			)
			return candidate, nil
		}
	}

	return nil, ErrPoolExhausted
}

// Release frees a previously allocated IP address.
func (a *Allocator) Release(ip net.IP) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := ip.String()
	for _, pool := range a.pools {
		if _, ok := pool.used[key]; ok {
			delete(pool.used, key)
			a.logger.Info("released LB IP",
				zap.String("ip", key),
				zap.String("pool", pool.Name),
			)
			return true
		}
	}
	return false
}

// GetServiceForIP returns the service key that owns the given IP.
func (a *Allocator) GetServiceForIP(ip net.IP) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	key := ip.String()
	for _, pool := range a.pools {
		if svc, ok := pool.used[key]; ok {
			return svc, true
		}
	}
	return "", false
}

// ListAllocations returns a snapshot of all current allocations
// as a map of IP string to service key.
func (a *Allocator) ListAllocations() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]string)
	for _, pool := range a.pools {
		for ip, svc := range pool.used {
			result[ip] = svc
		}
	}
	return result
}

// poolSize returns the number of host addresses in a CIDR block.
func poolSize(cidr *net.IPNet) uint64 {
	ones, bits := cidr.Mask.Size()
	hostBits := bits - ones
	if hostBits < 0 || hostBits > 63 {
		return 0
	}
	return 1 << uint64(hostBits)
}

// cloneIP returns a copy of an IP address.
func cloneIP(ip net.IP) net.IP {
	dup := make(net.IP, len(ip))
	copy(dup, ip)
	return dup
}

// addToIP adds an offset to a base IP address.
func addToIP(base net.IP, offset uint64) net.IP {
	ip := cloneIP(base).To16()
	if ip == nil {
		return nil
	}
	// Treat the last 8 bytes as a uint64 and add the offset.
	val := binary.BigEndian.Uint64(ip[8:16])
	val += offset
	result := make(net.IP, 16)
	copy(result[:8], ip[:8])
	binary.BigEndian.PutUint64(result[8:16], val)
	// Return as 4-byte IPv4 if the original was IPv4.
	if base.To4() != nil {
		return result.To4()
	}
	return result
}
