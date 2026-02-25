package routing

import (
	"context"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novanet/internal/config"
	"github.com/piwi3910/novanet/internal/node"
	"github.com/piwi3910/novanet/internal/novaroute"
	"github.com/piwi3910/novanet/internal/tunnel"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func testOverlayConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.RoutingMode = "overlay"
	cfg.TunnelProtocol = "geneve"
	return cfg
}

func testNativeConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.RoutingMode = "native"
	cfg.NovaRoute.Socket = "/run/novaroute/novaroute.sock"
	cfg.NovaRoute.Token = "test-token"
	cfg.NovaRoute.Protocol = "bgp"
	return cfg
}

func TestNewModeManager(t *testing.T) {
	cfg := testOverlayConfig()
	tunnelMgr := tunnel.NewManager("geneve", net.ParseIP("10.0.0.1"), 100, nil, testLogger())
	nodeReg := node.NewRegistry(testLogger())

	m := NewModeManager(cfg, tunnelMgr, nil, nodeReg, testLogger())
	if m == nil {
		t.Fatal("expected non-nil mode manager")
	}
	if m.Mode() != "overlay" {
		t.Fatalf("expected overlay mode, got %s", m.Mode())
	}
}

func TestOverlayModeStart(t *testing.T) {
	cfg := testOverlayConfig()
	tunnelMgr := tunnel.NewManager("geneve", net.ParseIP("10.0.0.1"), 100, nil, testLogger())
	nodeReg := node.NewRegistry(testLogger())

	m := NewModeManager(cfg, tunnelMgr, nil, nodeReg, testLogger())

	ctx := t.Context()

	err := m.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOverlayModeCreatesInitialTunnels(t *testing.T) {
	cfg := testOverlayConfig()
	tunnelMgr := tunnel.NewManager("geneve", net.ParseIP("10.0.0.1"), 100, nil, testLogger())
	nodeReg := node.NewRegistry(testLogger())

	// Add nodes before starting.
	nodeReg.AddNode("node-2", "10.0.0.2", "10.244.2.0/24")
	nodeReg.AddNode("node-3", "10.0.0.3", "10.244.3.0/24")

	m := NewModeManager(cfg, tunnelMgr, nil, nodeReg, testLogger())

	ctx := t.Context()

	err := m.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tunnels should be created for existing nodes.
	if tunnelMgr.Count() != 2 {
		t.Fatalf("expected 2 tunnels, got %d", tunnelMgr.Count())
	}
}

func TestOverlayModeReactsToNodeChanges(t *testing.T) {
	cfg := testOverlayConfig()
	tunnelMgr := tunnel.NewManager("geneve", net.ParseIP("10.0.0.1"), 100, nil, testLogger())
	nodeReg := node.NewRegistry(testLogger())

	m := NewModeManager(cfg, tunnelMgr, nil, nodeReg, testLogger())

	ctx := t.Context()

	err := m.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Add a node after starting.
	nodeReg.AddNode("node-2", "10.0.0.2", "10.244.2.0/24")

	// Give time for callback to fire.
	time.Sleep(50 * time.Millisecond)

	if tunnelMgr.Count() != 1 {
		t.Fatalf("expected 1 tunnel after node add, got %d", tunnelMgr.Count())
	}

	// Remove the node.
	nodeReg.RemoveNode("node-2")

	time.Sleep(50 * time.Millisecond)

	if tunnelMgr.Count() != 0 {
		t.Fatalf("expected 0 tunnels after node remove, got %d", tunnelMgr.Count())
	}
}

func TestOverlayModeStop(t *testing.T) {
	cfg := testOverlayConfig()
	tunnelMgr := tunnel.NewManager("geneve", net.ParseIP("10.0.0.1"), 100, nil, testLogger())
	nodeReg := node.NewRegistry(testLogger())

	nodeReg.AddNode("node-2", "10.0.0.2", "10.244.2.0/24")

	m := NewModeManager(cfg, tunnelMgr, nil, nodeReg, testLogger())

	ctx := t.Context()

	m.Start(ctx)

	err := m.Stop(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All tunnels should be removed.
	if tunnelMgr.Count() != 0 {
		t.Fatalf("expected 0 tunnels after stop, got %d", tunnelMgr.Count())
	}
}

func TestNativeModeRequiresClient(t *testing.T) {
	cfg := testNativeConfig()
	nodeReg := node.NewRegistry(testLogger())

	m := NewModeManager(cfg, nil, nil, nodeReg, testLogger())

	ctx := t.Context()

	err := m.Start(ctx)
	if err == nil {
		t.Fatal("expected error when NovaRoute client is nil")
	}
}

// mockRouteClient implements novaroute.RouteClient for testing.
type mockRouteClient struct {
	registerCalled  int
	advertiseCalled int
	withdrawCalled  int
	closeCalled     int
}

func (m *mockRouteClient) Register(ctx context.Context, owner, token string) error {
	m.registerCalled++
	return nil
}

func (m *mockRouteClient) AdvertisePrefix(ctx context.Context, owner, token, prefix, protocol string) error {
	m.advertiseCalled++
	return nil
}

func (m *mockRouteClient) WithdrawPrefix(ctx context.Context, owner, token, prefix, protocol string) error {
	m.withdrawCalled++
	return nil
}

func (m *mockRouteClient) StreamEvents(ctx context.Context, owner, token string, handler novaroute.EventHandler) error {
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockRouteClient) Close() error {
	m.closeCalled++
	return nil
}

func TestNativeModeStart(t *testing.T) {
	cfg := testNativeConfig()
	nodeReg := node.NewRegistry(testLogger())

	mock := &mockRouteClient{}
	nrClient := novaroute.NewClient(cfg.NovaRoute.Socket, "novanet", cfg.NovaRoute.Token, testLogger())
	nrClient.SetRouteClient(mock)

	m := NewModeManager(cfg, nil, nrClient, nodeReg, testLogger())

	ctx := t.Context()

	err := m.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.registerCalled != 1 {
		t.Fatalf("expected 1 register call, got %d", mock.registerCalled)
	}
	if mock.advertiseCalled != 1 {
		t.Fatalf("expected 1 advertise call, got %d", mock.advertiseCalled)
	}
}

func TestNativeModeStop(t *testing.T) {
	cfg := testNativeConfig()
	nodeReg := node.NewRegistry(testLogger())

	mock := &mockRouteClient{}
	nrClient := novaroute.NewClient(cfg.NovaRoute.Socket, "novanet", cfg.NovaRoute.Token, testLogger())
	nrClient.SetRouteClient(mock)

	m := NewModeManager(cfg, nil, nrClient, nodeReg, testLogger())

	ctx := t.Context()

	m.Start(ctx)

	err := m.Stop(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.withdrawCalled != 1 {
		t.Fatalf("expected 1 withdraw call, got %d", mock.withdrawCalled)
	}
	if mock.closeCalled != 1 {
		t.Fatalf("expected 1 close call, got %d", mock.closeCalled)
	}
}

func TestUnknownMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RoutingMode = "unknown"
	nodeReg := node.NewRegistry(testLogger())

	m := NewModeManager(cfg, nil, nil, nodeReg, testLogger())

	ctx := t.Context()

	err := m.Start(ctx)
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}
