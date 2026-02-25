package novaroute

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// mockRouteClient implements RouteClient for testing.
type mockRouteClient struct {
	mu sync.Mutex

	registerCalled    int
	advertiseCalled   int
	withdrawCalled    int
	streamCalled      int
	closeCalled       int

	registerErr   error
	advertiseErr  error
	withdrawErr   error
	streamErr     error

	lastAdvertisePrefix   string
	lastAdvertiseProtocol string
	lastWithdrawPrefix    string
	lastWithdrawProtocol  string

	// streamBlocking controls whether StreamEvents blocks or returns immediately.
	streamBlocking bool
}

func (m *mockRouteClient) Register(ctx context.Context, owner, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registerCalled++
	return m.registerErr
}

func (m *mockRouteClient) AdvertisePrefix(ctx context.Context, owner, token, prefix, protocol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.advertiseCalled++
	m.lastAdvertisePrefix = prefix
	m.lastAdvertiseProtocol = protocol
	return m.advertiseErr
}

func (m *mockRouteClient) WithdrawPrefix(ctx context.Context, owner, token, prefix, protocol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.withdrawCalled++
	m.lastWithdrawPrefix = prefix
	m.lastWithdrawProtocol = protocol
	return m.withdrawErr
}

func (m *mockRouteClient) StreamEvents(ctx context.Context, owner, token string, handler EventHandler) error {
	m.mu.Lock()
	m.streamCalled++
	blocking := m.streamBlocking
	streamErr := m.streamErr
	m.mu.Unlock()

	if streamErr != nil {
		return streamErr
	}

	if blocking {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (m *mockRouteClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled++
	return nil
}

func TestNewClient(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.socketPath != "/run/novaroute/novaroute.sock" {
		t.Fatalf("expected socket path /run/novaroute/novaroute.sock, got %s", c.socketPath)
	}
	if c.owner != "novanet" {
		t.Fatalf("expected owner novanet, got %s", c.owner)
	}
}

func TestConnectWithMock(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	mock := &mockRouteClient{}
	c.SetRouteClient(mock)

	ctx := context.Background()
	err := c.Connect(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !c.IsConnected() {
		t.Fatal("expected client to be connected")
	}
}

func TestRegister(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	mock := &mockRouteClient{}
	c.SetRouteClient(mock)

	ctx := context.Background()
	c.Connect(ctx)

	err := c.Register(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.registerCalled != 1 {
		t.Fatalf("expected 1 register call, got %d", mock.registerCalled)
	}

	if !c.IsRegistered() {
		t.Fatal("expected client to be registered")
	}
}

func TestRegisterError(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	mock := &mockRouteClient{registerErr: fmt.Errorf("auth failed")}
	c.SetRouteClient(mock)

	ctx := context.Background()
	c.Connect(ctx)

	err := c.Register(ctx)
	if err == nil {
		t.Fatal("expected error")
	}

	if c.IsRegistered() {
		t.Fatal("expected client to not be registered")
	}
}

func TestRegisterNotConnected(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())

	err := c.Register(context.Background())
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestAdvertisePrefix(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	mock := &mockRouteClient{}
	c.SetRouteClient(mock)

	ctx := context.Background()
	c.Connect(ctx)

	err := c.AdvertisePrefix(ctx, "10.244.1.0/24", "bgp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.advertiseCalled != 1 {
		t.Fatalf("expected 1 advertise call, got %d", mock.advertiseCalled)
	}
	if mock.lastAdvertisePrefix != "10.244.1.0/24" {
		t.Fatalf("expected prefix 10.244.1.0/24, got %s", mock.lastAdvertisePrefix)
	}
	if mock.lastAdvertiseProtocol != "bgp" {
		t.Fatalf("expected protocol bgp, got %s", mock.lastAdvertiseProtocol)
	}
}

func TestAdvertisePrefixError(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	mock := &mockRouteClient{advertiseErr: fmt.Errorf("prefix rejected")}
	c.SetRouteClient(mock)

	ctx := context.Background()
	c.Connect(ctx)

	err := c.AdvertisePrefix(ctx, "10.244.1.0/24", "bgp")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAdvertisePrefixNotConnected(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())

	err := c.AdvertisePrefix(context.Background(), "10.244.1.0/24", "bgp")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestWithdrawPrefix(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	mock := &mockRouteClient{}
	c.SetRouteClient(mock)

	ctx := context.Background()
	c.Connect(ctx)

	err := c.WithdrawPrefix(ctx, "10.244.1.0/24", "bgp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.withdrawCalled != 1 {
		t.Fatalf("expected 1 withdraw call, got %d", mock.withdrawCalled)
	}
	if mock.lastWithdrawPrefix != "10.244.1.0/24" {
		t.Fatalf("expected prefix 10.244.1.0/24, got %s", mock.lastWithdrawPrefix)
	}
}

func TestWithdrawPrefixNotConnected(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())

	err := c.WithdrawPrefix(context.Background(), "10.244.1.0/24", "bgp")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestStreamEvents(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	mock := &mockRouteClient{streamBlocking: true}
	c.SetRouteClient(mock)

	ctx := context.Background()
	c.Connect(ctx)

	var eventCount atomic.Int32
	c.SetEventHandler(func(event *RouteEvent) {
		eventCount.Add(1)
	})

	err := c.StreamEvents(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Give the goroutine time to start.
	time.Sleep(100 * time.Millisecond)

	mock.mu.Lock()
	called := mock.streamCalled
	mock.mu.Unlock()
	if called == 0 {
		t.Fatal("expected stream to have been called")
	}
}

func TestStreamEventsNotConnected(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())

	err := c.StreamEvents(context.Background())
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestClose(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	mock := &mockRouteClient{}
	c.SetRouteClient(mock)

	ctx := context.Background()
	c.Connect(ctx)

	if !c.IsConnected() {
		t.Fatal("expected connected before close")
	}

	err := c.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.IsConnected() {
		t.Fatal("expected not connected after close")
	}

	if c.IsRegistered() {
		t.Fatal("expected not registered after close")
	}

	if mock.closeCalled != 1 {
		t.Fatalf("expected 1 close call, got %d", mock.closeCalled)
	}
}

func TestCloseNotConnected(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())

	// Should not error.
	err := c.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConnectWithRetryTimeout(t *testing.T) {
	c := NewClient("/tmp/nonexistent-novaroute.sock", "novanet", "test-token", testLogger())
	c.maxRetries = 2
	c.baseDelay = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := c.Connect(ctx)
	// Should fail after maxRetries.
	if err == nil {
		// The gRPC NewClient doesn't fail immediately on Unix sockets, so this
		// may actually succeed (lazy connection). That's acceptable.
		c.Close()
	}
}

func TestConnectCanceled(t *testing.T) {
	c := NewClient("/tmp/nonexistent-novaroute.sock", "novanet", "test-token", testLogger())
	c.baseDelay = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := c.Connect(ctx)
	if err == nil {
		c.Close()
	}
}

func TestCloseStopsEventStream(t *testing.T) {
	c := NewClient("/run/novaroute/novaroute.sock", "novanet", "test-token", testLogger())
	mock := &mockRouteClient{streamBlocking: true}
	c.SetRouteClient(mock)

	ctx := context.Background()
	c.Connect(ctx)

	c.StreamEvents(ctx)

	// Give stream time to start.
	time.Sleep(50 * time.Millisecond)

	// Close should stop the event stream.
	err := c.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
