package cni

import (
	"context"
	"fmt"
	"net"
	"testing"

	"go.uber.org/zap"

	"github.com/piwi3910/novanet/internal/identity"
	"github.com/piwi3910/novanet/internal/ipam"
)

// mockDataplaneClient implements DataplaneClient for testing.
type mockDataplaneClient struct {
	endpoints    map[uint32]bool
	programs     map[string]bool
	upsertErr    error
	deleteErr    error
	attachErr    error
	upsertCalls  int
	deleteCalls  int
	attachCalls  int
}

func newMockDataplaneClient() *mockDataplaneClient {
	return &mockDataplaneClient{
		endpoints: make(map[uint32]bool),
		programs:  make(map[string]bool),
	}
}

func (m *mockDataplaneClient) UpsertEndpoint(ctx context.Context, ip uint32, ifindex int, mac net.HardwareAddr, identityID uint32, podName, namespace string, nodeIP uint32) error {
	m.upsertCalls++
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.endpoints[ip] = true
	return nil
}

func (m *mockDataplaneClient) DeleteEndpoint(ctx context.Context, ip uint32) error {
	m.deleteCalls++
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.endpoints, ip)
	return nil
}

func (m *mockDataplaneClient) AttachProgram(ctx context.Context, iface string, ingress bool) error {
	m.attachCalls++
	if m.attachErr != nil {
		return m.attachErr
	}
	direction := "egress"
	if ingress {
		direction = "ingress"
	}
	m.programs[iface+"/"+direction] = true
	return nil
}

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func testHandler(t *testing.T) (*Handler, *mockDataplaneClient) {
	t.Helper()

	ipamAlloc, err := ipam.NewAllocator("10.244.1.0/24")
	if err != nil {
		t.Fatalf("failed to create IPAM allocator: %v", err)
	}

	idAlloc := identity.NewAllocator(testLogger())
	dpClient := newMockDataplaneClient()
	handler := NewHandler(ipamAlloc, idAlloc, dpClient, testLogger())
	handler.SetNodeIP(0x0A000001) // 10.0.0.1

	return handler, dpClient
}

func TestAddPod(t *testing.T) {
	h, dp := testHandler(t)

	result, err := h.Add(&AddPodRequest{
		PodName:      "web-1",
		PodNamespace: "default",
		ContainerID:  "container-abc",
		Netns:        "/proc/123/ns/net",
		IfName:       "eth0",
		Labels:       map[string]string{"app": "web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IP == nil {
		t.Fatal("expected non-nil IP")
	}

	expectedIP := net.ParseIP("10.244.1.2").To4()
	if !result.IP.Equal(expectedIP) {
		t.Fatalf("expected IP %s, got %s", expectedIP, result.IP)
	}

	expectedGW := net.ParseIP("10.244.1.1").To4()
	if !result.Gateway.Equal(expectedGW) {
		t.Fatalf("expected gateway %s, got %s", expectedGW, result.Gateway)
	}

	if result.Mac == nil || len(result.Mac) != 6 {
		t.Fatal("expected 6-byte MAC address")
	}

	if result.PrefixLength != 24 {
		t.Fatalf("expected prefix length 24, got %d", result.PrefixLength)
	}

	// Check MAC has locally-administered bit set.
	if result.Mac[0]&0x02 == 0 {
		t.Fatal("expected locally-administered bit set in MAC")
	}

	// Check dataplane was called.
	if dp.upsertCalls != 1 {
		t.Fatalf("expected 1 upsert call, got %d", dp.upsertCalls)
	}
	if dp.attachCalls != 2 {
		t.Fatalf("expected 2 attach calls (ingress + egress), got %d", dp.attachCalls)
	}
}

func TestAddPodNilRequest(t *testing.T) {
	h, _ := testHandler(t)

	_, err := h.Add(nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestDelPod(t *testing.T) {
	h, dp := testHandler(t)

	// First add a pod.
	_, err := h.Add(&AddPodRequest{
		PodName:      "web-1",
		PodNamespace: "default",
		ContainerID:  "container-abc",
		Netns:        "/proc/123/ns/net",
		IfName:       "eth0",
		Labels:       map[string]string{"app": "web"},
	})
	if err != nil {
		t.Fatalf("unexpected error on add: %v", err)
	}

	// Now delete it.
	err = h.Del(&DelPodRequest{
		PodName:      "web-1",
		PodNamespace: "default",
		ContainerID:  "container-abc",
		Netns:        "/proc/123/ns/net",
		IfName:       "eth0",
	})
	if err != nil {
		t.Fatalf("unexpected error on del: %v", err)
	}

	if dp.deleteCalls != 1 {
		t.Fatalf("expected 1 delete call, got %d", dp.deleteCalls)
	}
}

func TestDelPodNilRequest(t *testing.T) {
	h, _ := testHandler(t)

	err := h.Del(nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestDelPodNotFound(t *testing.T) {
	h, _ := testHandler(t)

	// Deleting a non-existent pod should not error (idempotent).
	err := h.Del(&DelPodRequest{
		PodName:      "nonexistent",
		PodNamespace: "default",
		ContainerID:  "container-xyz",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddPodDataplaneUpsertError(t *testing.T) {
	h, dp := testHandler(t)

	dp.upsertErr = fmt.Errorf("dataplane unreachable")

	_, err := h.Add(&AddPodRequest{
		PodName:      "web-1",
		PodNamespace: "default",
		ContainerID:  "container-abc",
		Netns:        "/proc/123/ns/net",
		IfName:       "eth0",
		Labels:       map[string]string{"app": "web"},
	})
	if err == nil {
		t.Fatal("expected error when dataplane upsert fails")
	}
}

func TestAddPodDataplaneAttachError(t *testing.T) {
	h, dp := testHandler(t)

	dp.attachErr = fmt.Errorf("attach failed")

	_, err := h.Add(&AddPodRequest{
		PodName:      "web-1",
		PodNamespace: "default",
		ContainerID:  "container-abc",
		Netns:        "/proc/123/ns/net",
		IfName:       "eth0",
		Labels:       map[string]string{"app": "web"},
	})
	if err == nil {
		t.Fatal("expected error when attach fails")
	}

	// Should have cleaned up the endpoint.
	if dp.deleteCalls != 1 {
		t.Fatalf("expected 1 delete call for cleanup, got %d", dp.deleteCalls)
	}
}

func TestAddMultiplePods(t *testing.T) {
	h, _ := testHandler(t)

	result1, err := h.Add(&AddPodRequest{
		PodName:      "web-1",
		PodNamespace: "default",
		ContainerID:  "container-1",
		Netns:        "/proc/1/ns/net",
		IfName:       "eth0",
		Labels:       map[string]string{"app": "web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result2, err := h.Add(&AddPodRequest{
		PodName:      "web-2",
		PodNamespace: "default",
		ContainerID:  "container-2",
		Netns:        "/proc/2/ns/net",
		IfName:       "eth0",
		Labels:       map[string]string{"app": "web"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Each pod should get a different IP.
	if result1.IP.Equal(result2.IP) {
		t.Fatalf("expected different IPs, both got %s", result1.IP)
	}
}

func TestIPToUint32(t *testing.T) {
	tests := []struct {
		ip       string
		expected uint32
	}{
		{"10.0.0.1", 0x0A000001},
		{"10.244.1.2", 0x0AF40102},
		{"192.168.1.1", 0xC0A80101},
		{"0.0.0.0", 0x00000000},
		{"255.255.255.255", 0xFFFFFFFF},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		got := ipToUint32(ip)
		if got != tt.expected {
			t.Errorf("ipToUint32(%s) = 0x%08X, want 0x%08X", tt.ip, got, tt.expected)
		}
	}
}

func TestVethNameForPod(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		expected  string
	}{
		{"web", "default", "nv_default_web"},
		{"very-long-pod-name-that-exceeds-limit", "ns", "nv_ns_very-long"},
	}

	for _, tt := range tests {
		got := vethNameForPod(tt.name, tt.namespace)
		if got != tt.expected {
			t.Errorf("vethNameForPod(%s, %s) = %s, want %s", tt.name, tt.namespace, got, tt.expected)
		}
		if len(got) > 15 {
			t.Errorf("veth name %s exceeds 15 chars", got)
		}
	}
}

func TestGenerateMAC(t *testing.T) {
	mac, err := generateMAC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mac) != 6 {
		t.Fatalf("expected 6-byte MAC, got %d", len(mac))
	}

	// Locally administered bit should be set.
	if mac[0]&0x02 == 0 {
		t.Fatal("locally administered bit not set")
	}

	// Unicast bit should be set (multicast bit clear).
	if mac[0]&0x01 != 0 {
		t.Fatal("multicast bit is set, expected unicast")
	}
}
