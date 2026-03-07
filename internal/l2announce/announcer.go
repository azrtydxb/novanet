// Package l2announce handles Layer 2 announcements for LoadBalancer IPs.
// It sends Gratuitous ARP (IPv4) and Unsolicited Neighbor Advertisement
// (IPv6) packets to inform the local network about allocated LB IPs.
package l2announce

import (
	"fmt"
	"net"
	"sort"
	"sync"

	"go.uber.org/zap"
)

// Announcer manages a set of IPs and sends L2 announcements for them.
type Announcer struct {
	iface  string
	logger *zap.Logger
	mu     sync.RWMutex
	ips    map[string]bool // IP string -> active
}

// NewAnnouncer creates a new L2 announcer bound to the given network interface.
func NewAnnouncer(iface string, logger *zap.Logger) *Announcer {
	return &Announcer{
		iface:  iface,
		logger: logger,
		ips:    make(map[string]bool),
	}
}

// AddIP registers an IP for L2 announcement and sends an initial announcement.
func (a *Announcer) AddIP(ip net.IP) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := ip.String()
	if a.ips[key] {
		return nil // already announced
	}

	a.ips[key] = true
	a.logger.Info("added IP for L2 announcement",
		zap.String("ip", key),
		zap.String("interface", a.iface),
	)

	// Send initial announcement.
	if err := a.announceOne(ip); err != nil {
		a.logger.Warn("initial L2 announcement failed",
			zap.String("ip", key),
			zap.Error(err),
		)
		return err
	}
	return nil
}

// RemoveIP stops announcing the given IP.
func (a *Announcer) RemoveIP(ip net.IP) {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := ip.String()
	delete(a.ips, key)
	a.logger.Info("removed IP from L2 announcement",
		zap.String("ip", key),
		zap.String("interface", a.iface),
	)
}

// AnnounceAll sends GARP/NA for all registered IPs.
func (a *Announcer) AnnounceAll() error {
	a.mu.RLock()
	ips := make([]string, 0, len(a.ips))
	for ip := range a.ips {
		ips = append(ips, ip)
	}
	a.mu.RUnlock()

	var firstErr error
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if err := a.announceOne(ip); err != nil {
			a.logger.Warn("L2 announcement failed",
				zap.String("ip", ipStr),
				zap.Error(err),
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("announce %s: %w", ipStr, err)
			}
		}
	}
	return firstErr
}

// ListIPs returns a sorted list of all IPs being announced.
func (a *Announcer) ListIPs() []net.IP {
	a.mu.RLock()
	defer a.mu.RUnlock()

	keys := make([]string, 0, len(a.ips))
	for ip := range a.ips {
		keys = append(keys, ip)
	}
	sort.Strings(keys)

	result := make([]net.IP, 0, len(keys))
	for _, k := range keys {
		result = append(result, net.ParseIP(k))
	}
	return result
}

// announceOne sends the appropriate L2 announcement for a single IP.
// This calls platform-specific code (GARP on Linux, stub on other platforms).
func (a *Announcer) announceOne(ip net.IP) error {
	return sendGratuitousARP(a.iface, ip)
}
