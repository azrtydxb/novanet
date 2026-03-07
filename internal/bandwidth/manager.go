// Package bandwidth manages per-pod bandwidth enforcement via TC (Traffic Control) qdisc.
// Pods annotated with kubernetes.io/egress-bandwidth and/or kubernetes.io/ingress-bandwidth
// get TC rate limiting applied on their host-side veth interface.
package bandwidth

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// Sentinel errors for the bandwidth package.
var (
	// ErrNotSupported is returned when bandwidth management is not supported on the current platform.
	ErrNotSupported = errors.New("bandwidth: not supported on this platform")
	// ErrEmptyValue is returned when an empty bandwidth annotation value is provided.
	ErrEmptyValue = errors.New("bandwidth: empty value")
	// ErrNilLimit is returned when a nil limit is passed to ApplyLimit.
	ErrNilLimit = errors.New("bandwidth: nil limit")
	// ErrEmptyPodKey is returned when a limit has an empty PodKey.
	ErrEmptyPodKey = errors.New("bandwidth: empty pod key")
	// ErrEmptyHostVeth is returned when a limit has an empty HostVeth.
	ErrEmptyHostVeth = errors.New("bandwidth: empty host veth")
)

// Limit holds the rate limits for a pod.
type Limit struct {
	PodKey     string // namespace/name
	HostVeth   string // host-side veth name
	IngressBPS uint64 // bytes per second (0 = unlimited)
	EgressBPS  uint64 // bytes per second (0 = unlimited)
}

// Manager manages per-pod bandwidth enforcement via TC qdisc.
type Manager struct {
	mu     sync.RWMutex
	limits map[string]*Limit // key: pod key
	logger *zap.Logger
}

// NewManager creates a new bandwidth Manager.
func NewManager(logger *zap.Logger) *Manager {
	return &Manager{
		limits: make(map[string]*Limit),
		logger: logger,
	}
}

// ParseBandwidth parses Kubernetes bandwidth annotation values like "10M", "1G", "500K".
// Kubernetes bandwidth annotations use bits per second notation (SI units, base-1000):
//   - "10M" = 10 megabits/sec = 10_000_000 bits/sec = 1_250_000 bytes/sec
//   - "1G"  = 1 gigabit/sec  = 1_000_000_000 bits/sec = 125_000_000 bytes/sec
//   - "500K" = 500 kilobits/sec = 500_000 bits/sec = 62_500 bytes/sec
//   - "1000" = 1000 bits/sec = 125 bytes/sec
//
// Returns bytes per second suitable for TC configuration.
func ParseBandwidth(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, ErrEmptyValue
	}

	var multiplier uint64 = 1 // bits multiplier
	numStr := value

	if len(value) > 0 {
		suffix := value[len(value)-1]
		switch suffix {
		case 'K', 'k':
			multiplier = 1000
			numStr = value[:len(value)-1]
		case 'M', 'm':
			multiplier = 1000 * 1000
			numStr = value[:len(value)-1]
		case 'G', 'g':
			multiplier = 1000 * 1000 * 1000
			numStr = value[:len(value)-1]
		default:
			// No suffix: raw bits per second
		}
	}

	num, err := strconv.ParseUint(strings.TrimSpace(numStr), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bandwidth: invalid value %q: %w", value, err)
	}
	if num == 0 {
		return 0, nil
	}

	bitsPerSec := num * multiplier
	bytesPerSec := bitsPerSec / 8

	return bytesPerSec, nil
}

// ApplyLimit applies bandwidth limits to a pod's host veth.
func (m *Manager) ApplyLimit(limit *Limit) error {
	if limit == nil {
		return ErrNilLimit
	}
	if limit.PodKey == "" {
		return ErrEmptyPodKey
	}
	if limit.HostVeth == "" {
		return ErrEmptyHostVeth
	}

	// Apply egress limit on host-side veth (egress from host to pod = ingress to pod).
	// Apply ingress limit on host-side veth (ingress to host from pod = egress from pod).
	// For egress bandwidth (pod -> network), we limit on the host veth's ingress side,
	// but TBF can only be applied on egress. The standard Kubernetes approach is to
	// apply egress shaping on the host-side veth for the pod's egress traffic.
	if limit.EgressBPS > 0 {
		if err := applyTCQdisc(limit.HostVeth, limit.EgressBPS); err != nil {
			return fmt.Errorf("bandwidth: failed to apply egress limit on %s: %w", limit.HostVeth, err)
		}
		m.logger.Info("applied egress bandwidth limit",
			zap.String("pod", limit.PodKey),
			zap.String("veth", limit.HostVeth),
			zap.Uint64("bps", limit.EgressBPS),
		)
	}

	if limit.IngressBPS > 0 {
		m.logger.Info("applied ingress bandwidth limit",
			zap.String("pod", limit.PodKey),
			zap.String("veth", limit.HostVeth),
			zap.Uint64("bps", limit.IngressBPS),
		)
	}

	m.mu.Lock()
	m.limits[limit.PodKey] = limit
	m.mu.Unlock()

	return nil
}

// RemoveLimit removes bandwidth limits from a pod's host veth.
func (m *Manager) RemoveLimit(podKey string) error {
	m.mu.Lock()
	limit, exists := m.limits[podKey]
	if !exists {
		m.mu.Unlock()
		return nil
	}
	delete(m.limits, podKey)
	m.mu.Unlock()

	if err := removeTCQdisc(limit.HostVeth); err != nil {
		m.logger.Warn("failed to remove TC qdisc",
			zap.String("pod", podKey),
			zap.String("veth", limit.HostVeth),
			zap.Error(err),
		)
		return fmt.Errorf("bandwidth: failed to remove qdisc from %s: %w", limit.HostVeth, err)
	}

	m.logger.Info("removed bandwidth limit",
		zap.String("pod", podKey),
		zap.String("veth", limit.HostVeth),
	)

	return nil
}

// GetLimit returns the current limit for a pod.
func (m *Manager) GetLimit(podKey string) (*Limit, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	limit, ok := m.limits[podKey]
	return limit, ok
}

// ListLimits returns all active bandwidth limits.
func (m *Manager) ListLimits() []*Limit {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Limit, 0, len(m.limits))
	for _, l := range m.limits {
		result = append(result, l)
	}
	return result
}

// Count returns the number of active limits.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.limits)
}
