package dataplane

import (
	"context"
	"net"
	"testing"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/azrtydxb/novanet/api/v1"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func TestNewClient(t *testing.T) {
	c, err := NewClient("/run/novanet/dataplane.sock", testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientEmptySocket(t *testing.T) {
	_, err := NewClient("", testLogger())
	if err == nil {
		t.Fatal("expected error for empty socket path")
	}
}

func TestClientNotConnected(t *testing.T) {
	c, _ := NewClient("/run/novanet/dataplane.sock", testLogger())

	ctx := context.Background()

	err := c.UpsertEndpoint(ctx, &Endpoint{})
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.DeleteEndpoint(ctx, "")
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.UpsertPolicy(ctx, &PolicyRule{})
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.DeletePolicy(ctx, &PolicyRule{})
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	_, err = c.SyncPolicies(ctx, nil)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.UpsertTunnel(ctx, "", 0, 0)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.DeleteTunnel(ctx, "")
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.UpdateConfig(ctx, nil)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.AttachProgram(ctx, "eth0", AttachTCIngress)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.DetachProgram(ctx, "eth0", AttachTCIngress)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	_, err = c.StreamFlows(ctx, 0)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	_, err = c.GetStatus(ctx)
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestClientNotConnectedEBPFServices(t *testing.T) {
	c, _ := NewClient("/run/novanet/dataplane.sock", testLogger())

	ctx := context.Background()

	err := c.UpsertSockmapEndpoint(ctx, "10.0.0.1", 80)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.DeleteSockmapEndpoint(ctx, "10.0.0.1", 80)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	_, err = c.GetSockmapStats(ctx)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.UpsertMeshService(ctx, "10.0.0.1", 80, 15001)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.DeleteMeshService(ctx, "10.0.0.1", 80)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	_, err = c.ListMeshServices(ctx)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	err = c.UpdateRateLimitConfig(ctx, 100, 200, 1000000000)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	_, err = c.GetRateLimitStats(ctx)
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	_, err = c.GetBackendHealthStats(ctx, "", 0)
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestCloseNotConnected(t *testing.T) {
	c, _ := NewClient("/run/novanet/dataplane.sock", testLogger())

	err := c.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// mockDataplaneServer implements pb.DataplaneControlServer for testing.
type mockDataplaneServer struct {
	pb.UnimplementedDataplaneControlServer

	upsertEndpointCalled        int
	deleteEndpointCalled        int
	upsertPolicyCalled          int
	deletePolicyCalled          int
	syncPoliciesCalled          int
	upsertTunnelCalled          int
	deleteTunnelCalled          int
	updateConfigCalled          int
	attachCalled                int
	detachCalled                int
	getStatusCalled             int
	upsertSockmapCalled         int
	deleteSockmapCalled         int
	getSockmapStatsCalled       int
	upsertMeshCalled            int
	deleteMeshCalled            int
	listMeshCalled              int
	updateRateLimitCalled       int
	getRateLimitStatsCalled     int
	getBackendHealthStatsCalled int
}

func (m *mockDataplaneServer) UpsertEndpoint(_ context.Context, _ *pb.UpsertEndpointRequest) (*pb.UpsertEndpointResponse, error) {
	m.upsertEndpointCalled++
	return &pb.UpsertEndpointResponse{}, nil
}

func (m *mockDataplaneServer) DeleteEndpoint(_ context.Context, _ *pb.DeleteEndpointRequest) (*pb.DeleteEndpointResponse, error) {
	m.deleteEndpointCalled++
	return &pb.DeleteEndpointResponse{}, nil
}

func (m *mockDataplaneServer) UpsertPolicy(_ context.Context, _ *pb.UpsertPolicyRequest) (*pb.UpsertPolicyResponse, error) {
	m.upsertPolicyCalled++
	return &pb.UpsertPolicyResponse{}, nil
}

func (m *mockDataplaneServer) DeletePolicy(_ context.Context, _ *pb.DeletePolicyRequest) (*pb.DeletePolicyResponse, error) {
	m.deletePolicyCalled++
	return &pb.DeletePolicyResponse{}, nil
}

func (m *mockDataplaneServer) SyncPolicies(_ context.Context, req *pb.SyncPoliciesRequest) (*pb.SyncPoliciesResponse, error) {
	m.syncPoliciesCalled++
	return &pb.SyncPoliciesResponse{
		Added:   uint32(len(req.Policies)), //nolint:gosec // test code, len is always small
		Removed: 0,
		Updated: 0,
	}, nil
}

func (m *mockDataplaneServer) UpsertTunnel(_ context.Context, _ *pb.UpsertTunnelRequest) (*pb.UpsertTunnelResponse, error) {
	m.upsertTunnelCalled++
	return &pb.UpsertTunnelResponse{}, nil
}

func (m *mockDataplaneServer) DeleteTunnel(_ context.Context, _ *pb.DeleteTunnelRequest) (*pb.DeleteTunnelResponse, error) {
	m.deleteTunnelCalled++
	return &pb.DeleteTunnelResponse{}, nil
}

func (m *mockDataplaneServer) UpdateConfig(_ context.Context, _ *pb.UpdateConfigRequest) (*pb.UpdateConfigResponse, error) {
	m.updateConfigCalled++
	return &pb.UpdateConfigResponse{}, nil
}

func (m *mockDataplaneServer) AttachProgram(_ context.Context, _ *pb.AttachProgramRequest) (*pb.AttachProgramResponse, error) {
	m.attachCalled++
	return &pb.AttachProgramResponse{}, nil
}

func (m *mockDataplaneServer) DetachProgram(_ context.Context, _ *pb.DetachProgramRequest) (*pb.DetachProgramResponse, error) {
	m.detachCalled++
	return &pb.DetachProgramResponse{}, nil
}

func (m *mockDataplaneServer) GetDataplaneStatus(_ context.Context, _ *pb.GetDataplaneStatusRequest) (*pb.GetDataplaneStatusResponse, error) {
	m.getStatusCalled++
	return &pb.GetDataplaneStatusResponse{
		EndpointCount:  10,
		PolicyCount:    5,
		TunnelCount:    3,
		Mode:           "overlay",
		TunnelProtocol: "geneve",
	}, nil
}

func (m *mockDataplaneServer) UpsertSockmapEndpoint(_ context.Context, _ *pb.UpsertSockmapEndpointRequest) (*pb.UpsertSockmapEndpointResponse, error) {
	m.upsertSockmapCalled++
	return &pb.UpsertSockmapEndpointResponse{}, nil
}

func (m *mockDataplaneServer) DeleteSockmapEndpoint(_ context.Context, _ *pb.DeleteSockmapEndpointRequest) (*pb.DeleteSockmapEndpointResponse, error) {
	m.deleteSockmapCalled++
	return &pb.DeleteSockmapEndpointResponse{}, nil
}

func (m *mockDataplaneServer) GetSockmapStats(_ context.Context, _ *pb.GetInternalSockmapStatsRequest) (*pb.GetInternalSockmapStatsResponse, error) {
	m.getSockmapStatsCalled++
	return &pb.GetInternalSockmapStatsResponse{
		Redirected:      100,
		Fallback:        5,
		ActiveEndpoints: 3,
	}, nil
}

func (m *mockDataplaneServer) UpsertMeshService(_ context.Context, _ *pb.UpsertMeshServiceRequest) (*pb.UpsertMeshServiceResponse, error) {
	m.upsertMeshCalled++
	return &pb.UpsertMeshServiceResponse{}, nil
}

func (m *mockDataplaneServer) DeleteMeshService(_ context.Context, _ *pb.DeleteMeshServiceRequest) (*pb.DeleteMeshServiceResponse, error) {
	m.deleteMeshCalled++
	return &pb.DeleteMeshServiceResponse{}, nil
}

func (m *mockDataplaneServer) ListMeshServices(_ context.Context, _ *pb.ListInternalMeshServicesRequest) (*pb.ListInternalMeshServicesResponse, error) {
	m.listMeshCalled++
	return &pb.ListInternalMeshServicesResponse{
		Entries: []*pb.InternalMeshServiceEntry{
			{Ip: "10.0.0.1", Port: 80, RedirectPort: 15001},
			{Ip: "10.0.0.2", Port: 443, RedirectPort: 15001},
		},
	}, nil
}

func (m *mockDataplaneServer) UpdateRateLimitConfig(_ context.Context, _ *pb.UpdateRateLimitConfigRequest) (*pb.UpdateRateLimitConfigResponse, error) {
	m.updateRateLimitCalled++
	return &pb.UpdateRateLimitConfigResponse{}, nil
}

func (m *mockDataplaneServer) GetInternalRateLimitStats(_ context.Context, _ *pb.GetInternalRateLimitStatsRequest) (*pb.GetInternalRateLimitStatsResponse, error) {
	m.getRateLimitStatsCalled++
	return &pb.GetInternalRateLimitStatsResponse{
		Allowed: 1000,
		Denied:  50,
	}, nil
}

func (m *mockDataplaneServer) GetBackendHealthStats(_ context.Context, _ *pb.GetBackendHealthStatsRequest) (*pb.GetBackendHealthStatsResponse, error) {
	m.getBackendHealthStatsCalled++
	return &pb.GetBackendHealthStatsResponse{
		Backends: []*pb.InternalBackendHealthInfo{
			{
				Ip:           "10.0.0.1",
				Port:         8080,
				TotalConns:   100,
				FailedConns:  5,
				TimeoutConns: 2,
				SuccessConns: 93,
				AvgRttNs:     1500000,
				FailureRate:  0.05,
			},
		},
	}, nil
}

func startMockServer(t *testing.T) (*mockDataplaneServer, string) {
	t.Helper()

	mock := &mockDataplaneServer{}
	server := grpc.NewServer()
	pb.RegisterDataplaneControlServer(server, mock)

	// Create a TCP listener on a random port.
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go func() {
		_ = server.Serve(lis)
	}()

	t.Cleanup(func() {
		server.GracefulStop()
	})

	return mock, lis.Addr().String()
}

func connectTestClient(t *testing.T, addr string) *Client {
	t.Helper()

	c, err := NewClient("placeholder", testLogger())
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Bypass the Unix socket dial and connect directly to TCP.
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	c.conn = conn
	c.client = pb.NewDataplaneControlClient(conn)

	return c
}

func TestUpsertEndpoint(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.UpsertEndpoint(context.Background(), &Endpoint{
		IP:         "10.244.1.2",
		Ifindex:    42,
		MAC:        net.HardwareAddr{0x02, 0x00, 0x00, 0x01, 0x02, 0x03},
		IdentityID: 100,
		PodName:    "web-1",
		Namespace:  "default",
		NodeIP:     "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.upsertEndpointCalled != 1 {
		t.Fatalf("expected 1 upsert call, got %d", mock.upsertEndpointCalled)
	}
}

func TestDeleteEndpoint(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.DeleteEndpoint(context.Background(), "10.244.1.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.deleteEndpointCalled != 1 {
		t.Fatalf("expected 1 delete call, got %d", mock.deleteEndpointCalled)
	}
}

func TestUpsertPolicy(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.UpsertPolicy(context.Background(), &PolicyRule{
		SrcIdentity: 100,
		DstIdentity: 200,
		Protocol:    6,
		DstPort:     80,
		Action:      pb.PolicyAction_POLICY_ACTION_ALLOW,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.upsertPolicyCalled != 1 {
		t.Fatalf("expected 1 upsert call, got %d", mock.upsertPolicyCalled)
	}
}

func TestDeletePolicy(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.DeletePolicy(context.Background(), &PolicyRule{
		SrcIdentity: 100,
		DstIdentity: 200,
		Protocol:    6,
		DstPort:     80,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.deletePolicyCalled != 1 {
		t.Fatalf("expected 1 delete call, got %d", mock.deletePolicyCalled)
	}
}

func TestSyncPolicies(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	rules := []*PolicyRule{
		{SrcIdentity: 100, DstIdentity: 200, Protocol: 6, DstPort: 80, Action: pb.PolicyAction_POLICY_ACTION_ALLOW},
		{SrcIdentity: 100, DstIdentity: 300, Protocol: 17, DstPort: 53, Action: pb.PolicyAction_POLICY_ACTION_ALLOW},
	}

	result, err := c.SyncPolicies(context.Background(), rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.syncPoliciesCalled != 1 {
		t.Fatalf("expected 1 sync call, got %d", mock.syncPoliciesCalled)
	}

	if result.Added != 2 {
		t.Fatalf("expected 2 added, got %d", result.Added)
	}
}

func TestUpsertTunnel(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.UpsertTunnel(context.Background(), "10.0.0.2", 10, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.upsertTunnelCalled != 1 {
		t.Fatalf("expected 1 upsert call, got %d", mock.upsertTunnelCalled)
	}
}

func TestDeleteTunnel(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.DeleteTunnel(context.Background(), "10.0.0.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.deleteTunnelCalled != 1 {
		t.Fatalf("expected 1 delete call, got %d", mock.deleteTunnelCalled)
	}
}

func TestUpdateConfig(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.UpdateConfig(context.Background(), map[uint32]uint64{
		1: 100,
		2: 200,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.updateConfigCalled != 1 {
		t.Fatalf("expected 1 config call, got %d", mock.updateConfigCalled)
	}
}

func TestAttachDetachProgram(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.AttachProgram(context.Background(), "eth0", AttachTCIngress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = c.AttachProgram(context.Background(), "eth0", AttachTCEgress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.attachCalled != 2 {
		t.Fatalf("expected 2 attach calls, got %d", mock.attachCalled)
	}

	err = c.DetachProgram(context.Background(), "eth0", AttachTCIngress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.detachCalled != 1 {
		t.Fatalf("expected 1 detach call, got %d", mock.detachCalled)
	}
}

func TestGetStatus(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	status, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.getStatusCalled != 1 {
		t.Fatalf("expected 1 status call, got %d", mock.getStatusCalled)
	}

	if status.EndpointCount != 10 {
		t.Fatalf("expected 10 endpoints, got %d", status.EndpointCount)
	}
	if status.PolicyCount != 5 {
		t.Fatalf("expected 5 policies, got %d", status.PolicyCount)
	}
	if status.TunnelCount != 3 {
		t.Fatalf("expected 3 tunnels, got %d", status.TunnelCount)
	}
	if status.Mode != "overlay" {
		t.Fatalf("expected mode overlay, got %s", status.Mode)
	}
	if status.TunnelProtocol != "geneve" {
		t.Fatalf("expected protocol geneve, got %s", status.TunnelProtocol)
	}
}

func TestUpsertSockmapEndpoint(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.UpsertSockmapEndpoint(context.Background(), "10.0.0.1", 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.upsertSockmapCalled != 1 {
		t.Fatalf("expected 1 call, got %d", mock.upsertSockmapCalled)
	}
}

func TestDeleteSockmapEndpoint(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.DeleteSockmapEndpoint(context.Background(), "10.0.0.1", 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.deleteSockmapCalled != 1 {
		t.Fatalf("expected 1 call, got %d", mock.deleteSockmapCalled)
	}
}

func TestGetSockmapStats(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	stats, err := c.GetSockmapStats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.getSockmapStatsCalled != 1 {
		t.Fatalf("expected 1 call, got %d", mock.getSockmapStatsCalled)
	}

	if stats.Redirected != 100 {
		t.Fatalf("expected 100 redirected, got %d", stats.Redirected)
	}
	if stats.Fallback != 5 {
		t.Fatalf("expected 5 fallback, got %d", stats.Fallback)
	}
	if stats.ActiveEndpoints != 3 {
		t.Fatalf("expected 3 active endpoints, got %d", stats.ActiveEndpoints)
	}
}

func TestUpsertMeshService(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.UpsertMeshService(context.Background(), "10.0.0.1", 80, 15001)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.upsertMeshCalled != 1 {
		t.Fatalf("expected 1 call, got %d", mock.upsertMeshCalled)
	}
}

func TestDeleteMeshService(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.DeleteMeshService(context.Background(), "10.0.0.1", 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.deleteMeshCalled != 1 {
		t.Fatalf("expected 1 call, got %d", mock.deleteMeshCalled)
	}
}

func TestListMeshServices(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	entries, err := c.ListMeshServices(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.listMeshCalled != 1 {
		t.Fatalf("expected 1 call, got %d", mock.listMeshCalled)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].IP != "10.0.0.1" || entries[0].Port != 80 || entries[0].RedirectPort != 15001 {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
}

func TestUpdateRateLimitConfig(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	err := c.UpdateRateLimitConfig(context.Background(), 100, 200, 1000000000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.updateRateLimitCalled != 1 {
		t.Fatalf("expected 1 call, got %d", mock.updateRateLimitCalled)
	}
}

func TestGetRateLimitStats(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	stats, err := c.GetRateLimitStats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.getRateLimitStatsCalled != 1 {
		t.Fatalf("expected 1 call, got %d", mock.getRateLimitStatsCalled)
	}

	if stats.Allowed != 1000 {
		t.Fatalf("expected 1000 allowed, got %d", stats.Allowed)
	}
	if stats.Denied != 50 {
		t.Fatalf("expected 50 denied, got %d", stats.Denied)
	}
}

func TestGetBackendHealthStats(t *testing.T) {
	mock, addr := startMockServer(t)
	c := connectTestClient(t, addr)
	defer func() { _ = c.Close() }()

	backends, err := c.GetBackendHealthStats(context.Background(), "10.0.0.1", 8080)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.getBackendHealthStatsCalled != 1 {
		t.Fatalf("expected 1 call, got %d", mock.getBackendHealthStatsCalled)
	}

	if len(backends) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(backends))
	}

	b := backends[0]
	if b.IP != "10.0.0.1" || b.Port != 8080 {
		t.Fatalf("unexpected backend: %+v", b)
	}
	if b.TotalConns != 100 {
		t.Fatalf("expected 100 total conns, got %d", b.TotalConns)
	}
	if b.FailedConns != 5 {
		t.Fatalf("expected 5 failed conns, got %d", b.FailedConns)
	}
}

func TestClose(t *testing.T) {
	_, addr := startMockServer(t)
	c := connectTestClient(t, addr)

	err := c.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After close, operations should fail.
	err = c.UpsertEndpoint(context.Background(), &Endpoint{})
	if err == nil {
		t.Fatal("expected error after close")
	}
}
