// Package ebpfservices implements the EBPFServices gRPC server for managing
// kernel-level eBPF operations including sockmap acceleration, mesh traffic
// redirection, rate limiting, and backend health monitoring.
package ebpfservices

import (
	"context"

	pb "github.com/azrtydxb/novanet/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the EBPFServices gRPC service.
type Server struct {
	pb.UnimplementedEBPFServicesServer
	logger *zap.Logger
}

// NewServer creates a new EBPFServices server.
func NewServer(logger *zap.Logger) *Server {
	return &Server{logger: logger}
}

// EnableSockmap enables sockmap-based acceleration for a pod pair.
func (s *Server) EnableSockmap(_ context.Context, _ *pb.EnableSockmapRequest) (*pb.EnableSockmapResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// DisableSockmap disables sockmap-based acceleration for a pod pair.
func (s *Server) DisableSockmap(_ context.Context, _ *pb.DisableSockmapRequest) (*pb.DisableSockmapResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// GetSockmapStats returns sockmap statistics.
func (s *Server) GetSockmapStats(_ context.Context, _ *pb.GetSockmapStatsRequest) (*pb.GetSockmapStatsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// AddMeshRedirect adds a mesh traffic redirection rule.
func (s *Server) AddMeshRedirect(_ context.Context, _ *pb.AddMeshRedirectRequest) (*pb.AddMeshRedirectResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// RemoveMeshRedirect removes a mesh traffic redirection rule.
func (s *Server) RemoveMeshRedirect(_ context.Context, _ *pb.RemoveMeshRedirectRequest) (*pb.RemoveMeshRedirectResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// ListMeshRedirects lists all active mesh redirection rules.
func (s *Server) ListMeshRedirects(_ context.Context, _ *pb.ListMeshRedirectsRequest) (*pb.ListMeshRedirectsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// ConfigureRateLimit configures kernel-level per-source-IP rate limiting.
func (s *Server) ConfigureRateLimit(_ context.Context, _ *pb.ConfigureRateLimitRequest) (*pb.ConfigureRateLimitResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// RemoveRateLimit removes a rate limit configuration.
func (s *Server) RemoveRateLimit(_ context.Context, _ *pb.RemoveRateLimitRequest) (*pb.RemoveRateLimitResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// GetRateLimitStats returns rate limit statistics.
func (s *Server) GetRateLimitStats(_ context.Context, _ *pb.GetRateLimitStatsRequest) (*pb.GetRateLimitStatsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// GetBackendHealth returns passive TCP health counters for backends.
func (s *Server) GetBackendHealth(_ context.Context, _ *pb.GetBackendHealthRequest) (*pb.GetBackendHealthResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// StreamBackendHealth streams backend health events.
func (s *Server) StreamBackendHealth(_ *pb.StreamBackendHealthRequest, _ grpc.ServerStreamingServer[pb.BackendHealthEvent]) error {
	return status.Errorf(codes.Unimplemented, "not yet implemented")
}
