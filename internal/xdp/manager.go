// Package xdp manages XDP (eXpress Data Path) program attachment on
// physical interfaces for accelerated L4 service lookup.
package xdp

import (
	"errors"
	"net"
	"sync"

	"go.uber.org/zap"
)

// Mode determines how XDP programs are attached.
type Mode string

const (
	// ModeDisabled disables XDP acceleration.
	ModeDisabled Mode = "disabled"
	// ModeNative attaches XDP in native/driver mode (requires driver support).
	ModeNative Mode = "native"
	// ModeBestEffort tries native mode, falls back to SKB/generic mode.
	ModeBestEffort Mode = "best-effort"
)

// Sentinel errors.
var (
	ErrDisabled     = errors.New("xdp: acceleration is disabled")
	ErrNoInterfaces = errors.New("xdp: no eligible interfaces found")
)

// AttachFunc is called to attach an XDP program to an interface via the dataplane.
type AttachFunc func(iface string, native bool) error

// DetachFunc is called to detach an XDP program from an interface.
type DetachFunc func(iface string) error

// Manager manages XDP program attachment on physical interfaces.
type Manager struct {
	mu         sync.RWMutex
	mode       Mode
	attachFunc AttachFunc
	detachFunc DetachFunc
	attached   map[string]bool // interface name -> attached
	logger     *zap.Logger
}

// NewManager creates a new XDP manager.
func NewManager(mode Mode, attachFn AttachFunc, detachFn DetachFunc, logger *zap.Logger) *Manager {
	return &Manager{
		mode:       mode,
		attachFunc: attachFn,
		detachFunc: detachFn,
		attached:   make(map[string]bool),
		logger:     logger,
	}
}

// AttachAll discovers physical interfaces and attaches XDP programs.
func (m *Manager) AttachAll() error {
	if m.mode == ModeDisabled {
		return ErrDisabled
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return err
	}

	var eligible []string
	for _, iface := range ifaces {
		// Skip loopback, virtual, and down interfaces.
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		// Skip common virtual interface prefixes.
		if isVirtualInterface(iface.Name) {
			continue
		}
		eligible = append(eligible, iface.Name)
	}

	if len(eligible) == 0 {
		return ErrNoInterfaces
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	native := m.mode == ModeNative
	for _, name := range eligible {
		if err := m.attachFunc(name, native); err != nil {
			if m.mode == ModeBestEffort {
				// Retry with SKB/generic mode.
				if err2 := m.attachFunc(name, false); err2 != nil {
					m.logger.Warn("failed to attach XDP (best-effort)",
						zap.String("iface", name), zap.Error(err2))
					continue
				}
			} else {
				m.logger.Warn("failed to attach XDP",
					zap.String("iface", name), zap.Error(err))
				continue
			}
		}
		m.attached[name] = true
		m.logger.Info("attached XDP program",
			zap.String("iface", name),
			zap.String("mode", string(m.mode)),
		)
	}

	return nil
}

// DetachAll detaches XDP programs from all interfaces.
func (m *Manager) DetachAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name := range m.attached {
		if err := m.detachFunc(name); err != nil {
			m.logger.Warn("failed to detach XDP",
				zap.String("iface", name), zap.Error(err))
		}
		m.logger.Info("detached XDP program", zap.String("iface", name))
	}
	m.attached = make(map[string]bool)
}

// AttachedInterfaces returns the list of interfaces with XDP attached.
func (m *Manager) AttachedInterfaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, 0, len(m.attached))
	for name := range m.attached {
		result = append(result, name)
	}
	return result
}

// IsEnabled returns true if XDP acceleration is not disabled.
func (m *Manager) IsEnabled() bool {
	return m.mode != ModeDisabled
}

// isVirtualInterface returns true for common virtual interface name prefixes.
func isVirtualInterface(name string) bool {
	prefixes := []string{
		"veth", "docker", "br-", "cni", "flannel",
		"cali", "tunl", "genev", "vxlan", "nv", "lo",
		"novanet", "wg",
	}
	for _, p := range prefixes {
		if len(name) >= len(p) && name[:len(p)] == p {
			return true
		}
	}
	return false
}
