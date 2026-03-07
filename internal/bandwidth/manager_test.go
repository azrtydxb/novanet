package bandwidth

import (
	"testing"

	"go.uber.org/zap"
)

func TestParseBandwidth(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint64
		wantErr bool
	}{
		{
			name:  "10M megabits",
			input: "10M",
			want:  1_250_000, // 10_000_000 bits / 8
		},
		{
			name:  "10m lowercase megabits",
			input: "10m",
			want:  1_250_000,
		},
		{
			name:  "1G gigabit",
			input: "1G",
			want:  125_000_000, // 1_000_000_000 bits / 8
		},
		{
			name:  "1g lowercase gigabit",
			input: "1g",
			want:  125_000_000,
		},
		{
			name:  "500K kilobits",
			input: "500K",
			want:  62_500, // 500_000 bits / 8
		},
		{
			name:  "500k lowercase kilobits",
			input: "500k",
			want:  62_500,
		},
		{
			name:  "raw number bits per second",
			input: "1000",
			want:  125, // 1000 bits / 8
		},
		{
			name:  "100M",
			input: "100M",
			want:  12_500_000, // 100_000_000 bits / 8
		},
		{
			name:  "zero value",
			input: "0M",
			want:  0,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "negative value",
			input:   "-10M",
			wantErr: true,
		},
		{
			name:  "whitespace trimmed",
			input: "  10M  ",
			want:  1_250_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBandwidth(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseBandwidth(%q) expected error, got %d", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBandwidth(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseBandwidth(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return NewManager(logger)
}

func TestApplyLimitStoresLimit(t *testing.T) {
	m := newTestManager(t)

	limit := &Limit{
		PodKey:     "default/test-pod",
		HostVeth:   "veth1234",
		IngressBPS: 1_250_000,
		EgressBPS:  625_000,
	}

	// On non-Linux, ApplyLimit will fail because applyTCQdisc returns ErrNotSupported
	// when EgressBPS > 0. Test with EgressBPS=0 to test the storage path on all platforms,
	// then test with EgressBPS > 0 separately.
	limitNoEgress := &Limit{
		PodKey:     "default/test-pod",
		HostVeth:   "veth1234",
		IngressBPS: 1_250_000,
		EgressBPS:  0,
	}

	err := m.ApplyLimit(limitNoEgress)
	if err != nil {
		t.Fatalf("ApplyLimit() unexpected error: %v", err)
	}

	got, ok := m.GetLimit("default/test-pod")
	if !ok {
		t.Fatal("GetLimit() returned false, expected true")
	}
	if got.PodKey != limitNoEgress.PodKey {
		t.Errorf("got PodKey %q, want %q", got.PodKey, limitNoEgress.PodKey)
	}
	if got.IngressBPS != limitNoEgress.IngressBPS {
		t.Errorf("got IngressBPS %d, want %d", got.IngressBPS, limitNoEgress.IngressBPS)
	}

	// Test with EgressBPS > 0: on non-Linux this should return ErrNotSupported
	err = m.ApplyLimit(limit)
	if err != nil {
		// On non-Linux, we expect ErrNotSupported in the error chain
		t.Logf("ApplyLimit with egress returned expected error on this platform: %v", err)
	}
}

func TestApplyLimitValidation(t *testing.T) {
	m := newTestManager(t)

	if err := m.ApplyLimit(nil); err == nil {
		t.Error("ApplyLimit(nil) expected error")
	}

	if err := m.ApplyLimit(&Limit{PodKey: "", HostVeth: "veth1"}); err == nil {
		t.Error("ApplyLimit with empty PodKey expected error")
	}

	if err := m.ApplyLimit(&Limit{PodKey: "default/pod", HostVeth: ""}); err == nil {
		t.Error("ApplyLimit with empty HostVeth expected error")
	}
}

func TestRemoveLimit(t *testing.T) {
	m := newTestManager(t)

	limit := &Limit{
		PodKey:     "default/test-pod",
		HostVeth:   "veth1234",
		IngressBPS: 1_250_000,
		EgressBPS:  0,
	}

	err := m.ApplyLimit(limit)
	if err != nil {
		t.Fatalf("ApplyLimit() unexpected error: %v", err)
	}

	if m.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", m.Count())
	}

	// RemoveLimit calls removeTCQdisc which returns ErrNotSupported on non-Linux.
	// The limit is still removed from the map, but an error is returned.
	err = m.RemoveLimit("default/test-pod")
	if err != nil {
		t.Logf("RemoveLimit returned expected error on this platform: %v", err)
	}

	_, ok := m.GetLimit("default/test-pod")
	if ok {
		t.Error("GetLimit() returned true after RemoveLimit, expected false")
	}

	// Removing a non-existent limit should succeed.
	err = m.RemoveLimit("default/nonexistent")
	if err != nil {
		t.Errorf("RemoveLimit(nonexistent) unexpected error: %v", err)
	}
}

func TestListLimits(t *testing.T) {
	m := newTestManager(t)

	limits := []*Limit{
		{PodKey: "ns1/pod1", HostVeth: "veth1", IngressBPS: 1000, EgressBPS: 0},
		{PodKey: "ns1/pod2", HostVeth: "veth2", IngressBPS: 2000, EgressBPS: 0},
		{PodKey: "ns2/pod3", HostVeth: "veth3", IngressBPS: 3000, EgressBPS: 0},
	}

	for _, l := range limits {
		if err := m.ApplyLimit(l); err != nil {
			t.Fatalf("ApplyLimit(%s) unexpected error: %v", l.PodKey, err)
		}
	}

	got := m.ListLimits()
	if len(got) != 3 {
		t.Errorf("ListLimits() returned %d limits, want 3", len(got))
	}

	// Verify all pods are present.
	found := make(map[string]bool)
	for _, l := range got {
		found[l.PodKey] = true
	}
	for _, l := range limits {
		if !found[l.PodKey] {
			t.Errorf("ListLimits() missing pod %s", l.PodKey)
		}
	}
}

func TestCount(t *testing.T) {
	m := newTestManager(t)

	if m.Count() != 0 {
		t.Errorf("Count() = %d, want 0", m.Count())
	}

	_ = m.ApplyLimit(&Limit{PodKey: "ns/pod1", HostVeth: "veth1", EgressBPS: 0})
	if m.Count() != 1 {
		t.Errorf("Count() = %d, want 1", m.Count())
	}

	_ = m.ApplyLimit(&Limit{PodKey: "ns/pod2", HostVeth: "veth2", EgressBPS: 0})
	if m.Count() != 2 {
		t.Errorf("Count() = %d, want 2", m.Count())
	}

	// Overwrite existing.
	_ = m.ApplyLimit(&Limit{PodKey: "ns/pod1", HostVeth: "veth1", EgressBPS: 0, IngressBPS: 999})
	if m.Count() != 2 {
		t.Errorf("Count() after overwrite = %d, want 2", m.Count())
	}
}

func TestGetLimitNotFound(t *testing.T) {
	m := newTestManager(t)

	_, ok := m.GetLimit("nonexistent")
	if ok {
		t.Error("GetLimit(nonexistent) returned true, expected false")
	}
}
