package xdp

import (
	"errors"
	"testing"

	"go.uber.org/zap"
)

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager(ModeDisabled, nil, nil, logger)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.IsEnabled() {
		t.Error("disabled manager should not be enabled")
	}
}

func TestIsEnabled(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		mode    Mode
		enabled bool
	}{
		{ModeDisabled, false},
		{ModeNative, true},
		{ModeBestEffort, true},
	}

	for _, tt := range tests {
		m := NewManager(tt.mode, nil, nil, logger)
		if got := m.IsEnabled(); got != tt.enabled {
			t.Errorf("mode %q: IsEnabled() = %v, want %v", tt.mode, got, tt.enabled)
		}
	}
}

func TestAttachAllDisabled(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager(ModeDisabled, nil, nil, logger)
	err := m.AttachAll()
	if !errors.Is(err, ErrDisabled) {
		t.Errorf("expected ErrDisabled, got %v", err)
	}
}

func TestAttachAndDetach(t *testing.T) {
	logger := zap.NewNop()

	var attachedIfaces []string
	attachFn := func(iface string, native bool) error {
		attachedIfaces = append(attachedIfaces, iface)
		return nil
	}
	var detachedIfaces []string
	detachFn := func(iface string) error {
		detachedIfaces = append(detachedIfaces, iface)
		return nil
	}

	m := NewManager(ModeNative, attachFn, detachFn, logger)

	// AttachAll will try to find physical interfaces.
	// On test machines there might not be any non-virtual interfaces.
	_ = m.AttachAll()

	// Manually add an attached interface for detach test.
	m.mu.Lock()
	m.attached["test0"] = true
	m.mu.Unlock()

	ifaces := m.AttachedInterfaces()
	if len(ifaces) == 0 {
		t.Error("expected at least test0 in attached interfaces")
	}

	m.DetachAll()
	if len(m.AttachedInterfaces()) != 0 {
		t.Error("expected no attached interfaces after DetachAll")
	}
}

func TestIsVirtualInterface(t *testing.T) {
	tests := []struct {
		name    string
		virtual bool
	}{
		{"eth0", false},
		{"ens3", false},
		{"enp0s1", false},
		{"veth1234", true},
		{"docker0", true},
		{"br-abc", true},
		{"cni0", true},
		{"lo", true},
		{"novanet-wg0", true},
	}

	for _, tt := range tests {
		if got := isVirtualInterface(tt.name); got != tt.virtual {
			t.Errorf("isVirtualInterface(%q) = %v, want %v", tt.name, got, tt.virtual)
		}
	}
}
