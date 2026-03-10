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

// defaultMaxEntries is the default maximum number of entries in the DNS cache.
const defaultMaxEntries = 10000

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

// dnsCacheEntry holds a cached DNS result along with LRU tracking metadata.
type dnsCacheEntry struct {
	ips        []net.IP
	expiry     time.Time
	lastAccess time.Time
}

// DNSCache caches DNS resolution results for FQDN-based policy peers.
// It stores resolved IPs and their TTLs, supports periodic refresh, and
// enforces a maximum size with LRU eviction.
type DNSCache struct {
	mu         sync.RWMutex
	entries    map[string]*dnsCacheEntry
	maxEntries int
	logger     *zap.Logger
	resolver   DNSResolver
}

// NewDNSCache creates a new DNS cache with the given maximum number of entries.
// If maxEntries is <= 0, defaultMaxEntries (10000) is used.
func NewDNSCache(logger *zap.Logger, maxEntries int) *DNSCache {
	if maxEntries <= 0 {
		maxEntries = defaultMaxEntries
	}
	return &DNSCache{
		entries:    make(map[string]*dnsCacheEntry),
		maxEntries: maxEntries,
		logger:     logger,
		resolver:   defaultResolver,
	}
}

// SetResolver overrides the DNS resolution function. This is primarily
// useful for testing.
func (c *DNSCache) SetResolver(resolver DNSResolver) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resolver = resolver
}

// Size returns the current number of entries in the cache.
func (c *DNSCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Resolve returns the cached IPs for the given FQDN, performing a lookup
// if the entry is missing or expired.
func (c *DNSCache) Resolve(fqdn string) []net.IP {
	c.mu.RLock()
	entry, ok := c.entries[fqdn]
	c.mu.RUnlock()

	if ok && time.Now().Before(entry.expiry) {
		c.mu.Lock()
		entry.lastAccess = time.Now()
		c.mu.Unlock()
		return entry.ips
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
			return entry.ips
		}
		return nil
	}

	now := time.Now()

	c.mu.Lock()
	c.entries[fqdn] = &dnsCacheEntry{
		ips:        resolved,
		expiry:     now.Add(defaultDNSTTL),
		lastAccess: now,
	}
	c.evictLocked()
	c.mu.Unlock()

	c.logger.Debug("resolved FQDN",
		zap.String("fqdn", fqdn),
		zap.Int("ip_count", len(resolved)),
	)

	return resolved
}

// evictLocked removes the least recently used entry if the cache exceeds maxEntries.
// Must be called with c.mu held for writing.
func (c *DNSCache) evictLocked() {
	for len(c.entries) > c.maxEntries {
		var oldestKey string
		var oldestTime time.Time
		first := true

		for key, entry := range c.entries {
			if first || entry.lastAccess.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.lastAccess
				first = false
			}
		}

		c.logger.Debug("evicting DNS cache entry (LRU)",
			zap.String("fqdn", oldestKey),
			zap.Int("cache_size", len(c.entries)),
		)
		delete(c.entries, oldestKey)
	}
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

		now := time.Now()
		c.mu.Lock()
		entry := c.entries[fqdn]
		var old []net.IP
		if entry != nil {
			old = entry.ips
		}
		if !ipsEqual(old, resolved) {
			changed++
			c.logger.Info("DNS entry changed on refresh",
				zap.String("fqdn", fqdn),
				zap.Int("old_count", len(old)),
				zap.Int("new_count", len(resolved)),
			)
		}
		c.entries[fqdn] = &dnsCacheEntry{
			ips:        resolved,
			expiry:     now.Add(defaultDNSTTL),
			lastAccess: now,
		}
		c.mu.Unlock()
	}

	return changed
}

// GetAll returns a snapshot of all cached FQDN -> IP mappings.
func (c *DNSCache) GetAll() map[string][]net.IP {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string][]net.IP, len(c.entries))
	for fqdn, entry := range c.entries {
		ipsCopy := make([]net.IP, len(entry.ips))
		copy(ipsCopy, entry.ips)
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
