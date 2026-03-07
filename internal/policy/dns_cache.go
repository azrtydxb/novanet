package policy

import (
	"context"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// defaultDNSTTL is the default time-to-live for cached DNS entries.
const defaultDNSTTL = 5 * time.Minute

// DNSResolver is the function signature for resolving hostnames to IPs.
// It exists to allow injection of test doubles.
type DNSResolver func(ctx context.Context, host string) ([]net.IP, error)

// defaultResolver performs a real DNS lookup using the standard resolver with context.
func defaultResolver(ctx context.Context, host string) ([]net.IP, error) {
	var resolver net.Resolver
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, len(addrs))
	for i, addr := range addrs {
		ips[i] = addr.IP
	}
	return ips, nil
}

// DNSCache caches DNS resolution results for FQDN-based policy peers.
// It stores resolved IPs and their TTLs, and supports periodic refresh.
type DNSCache struct {
	mu       sync.RWMutex
	entries  map[string][]net.IP  // FQDN -> resolved IPs
	ttls     map[string]time.Time // FQDN -> expiry
	logger   *zap.Logger
	resolver DNSResolver
}

// NewDNSCache creates a new DNS cache.
func NewDNSCache(logger *zap.Logger) *DNSCache {
	return &DNSCache{
		entries:  make(map[string][]net.IP),
		ttls:     make(map[string]time.Time),
		logger:   logger,
		resolver: defaultResolver,
	}
}

// SetResolver overrides the DNS resolution function. This is primarily
// useful for testing.
func (c *DNSCache) SetResolver(resolver DNSResolver) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resolver = resolver
}

// Resolve returns the cached IPs for the given FQDN, performing a lookup
// if the entry is missing or expired.
func (c *DNSCache) Resolve(fqdn string) []net.IP {
	c.mu.RLock()
	ips, ok := c.entries[fqdn]
	expiry, hasExpiry := c.ttls[fqdn]
	c.mu.RUnlock()

	if ok && hasExpiry && time.Now().Before(expiry) {
		return ips
	}

	// Cache miss or expired — resolve.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resolved, err := c.resolver(ctx, fqdn)
	if err != nil {
		c.logger.Warn("DNS resolution failed",
			zap.String("fqdn", fqdn),
			zap.Error(err),
		)
		// Return stale data if available.
		if ok {
			return ips
		}
		return nil
	}

	c.mu.Lock()
	c.entries[fqdn] = resolved
	c.ttls[fqdn] = time.Now().Add(defaultDNSTTL)
	c.mu.Unlock()

	c.logger.Debug("resolved FQDN",
		zap.String("fqdn", fqdn),
		zap.Int("ip_count", len(resolved)),
	)

	return resolved
}

// Refresh re-resolves all cached FQDNs and returns the number of entries
// whose resolved IPs changed.
func (c *DNSCache) Refresh() int {
	c.mu.RLock()
	fqdns := make([]string, 0, len(c.entries))
	for fqdn := range c.entries {
		fqdns = append(fqdns, fqdn)
	}
	c.mu.RUnlock()

	changed := 0
	for _, fqdn := range fqdns {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		resolved, err := c.resolver(ctx, fqdn)
		cancel()

		if err != nil {
			c.logger.Warn("DNS refresh failed",
				zap.String("fqdn", fqdn),
				zap.Error(err),
			)
			continue
		}

		c.mu.Lock()
		old := c.entries[fqdn]
		if !ipsEqual(old, resolved) {
			changed++
			c.logger.Info("DNS entry changed on refresh",
				zap.String("fqdn", fqdn),
				zap.Int("old_count", len(old)),
				zap.Int("new_count", len(resolved)),
			)
		}
		c.entries[fqdn] = resolved
		c.ttls[fqdn] = time.Now().Add(defaultDNSTTL)
		c.mu.Unlock()
	}

	return changed
}

// GetAll returns a snapshot of all cached FQDN -> IP mappings.
func (c *DNSCache) GetAll() map[string][]net.IP {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string][]net.IP, len(c.entries))
	for fqdn, ips := range c.entries {
		ipsCopy := make([]net.IP, len(ips))
		copy(ipsCopy, ips)
		result[fqdn] = ipsCopy
	}
	return result
}

// ipsEqual returns true if two IP slices contain the same IPs (order-insensitive).
func ipsEqual(a, b []net.IP) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, ip := range a {
		set[ip.String()] = struct{}{}
	}
	for _, ip := range b {
		if _, ok := set[ip.String()]; !ok {
			return false
		}
	}
	return true
}
