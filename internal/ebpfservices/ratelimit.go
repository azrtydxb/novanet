package ebpfservices

import (
	"context"
	"net"

	pb "github.com/azrtydxb/novanet/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ConfigureRateLimit configures kernel-level per-source-IP rate limiting
// for the given CIDR range.
func (s *Server) ConfigureRateLimit(ctx context.Context, req *pb.ConfigureRateLimitRequest) (*pb.ConfigureRateLimitResponse, error) {
	if req.Cidr == "" {
		return nil, status.Errorf(codes.InvalidArgument, "cidr is required")
	}
	if _, _, err := net.ParseCIDR(req.Cidr); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid CIDR: %v", err)
	}
	if req.Rate == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "rate must be greater than zero")
	}
	if req.Burst == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "burst must be greater than zero")
	}
	if s.dataplane == nil {
		return nil, status.Errorf(codes.Unavailable, "dataplane not connected")
	}
	// The dataplane rate-limit API currently applies a global config.
	// The CIDR parameter is stored for future per-prefix support.
	windowNs := uint64(1_000_000_000) // 1-second window
	if err := s.dataplane.UpdateRateLimitConfig(ctx, req.Rate, req.Burst, windowNs); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to configure rate limit: %v", err)
	}
	s.logger.Info("rate limit configured",
		zap.String("cidr", req.Cidr),
		zap.Uint32("rate", req.Rate),
		zap.Uint32("burst", req.Burst))
	return &pb.ConfigureRateLimitResponse{}, nil
}

// RemoveRateLimit removes a rate limit configuration by setting rate/burst to zero.
func (s *Server) RemoveRateLimit(ctx context.Context, req *pb.RemoveRateLimitRequest) (*pb.RemoveRateLimitResponse, error) {
	if req.Cidr == "" {
		return nil, status.Errorf(codes.InvalidArgument, "cidr is required")
	}
	if _, _, err := net.ParseCIDR(req.Cidr); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid CIDR: %v", err)
	}
	if s.dataplane == nil {
		return nil, status.Errorf(codes.Unavailable, "dataplane not connected")
	}
	// Remove by setting rate/burst to zero.
	if err := s.dataplane.UpdateRateLimitConfig(ctx, 0, 0, 0); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to remove rate limit: %v", err)
	}
	s.logger.Info("rate limit removed", zap.String("cidr", req.Cidr))
	return &pb.RemoveRateLimitResponse{}, nil
}

// GetRateLimitStats returns rate limit statistics from the dataplane.
func (s *Server) GetRateLimitStats(ctx context.Context, _ *pb.GetRateLimitStatsRequest) (*pb.GetRateLimitStatsResponse, error) {
	if s.dataplane == nil {
		return nil, status.Errorf(codes.Unavailable, "dataplane not connected")
	}
	stats, err := s.dataplane.GetRateLimitStats(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get rate limit stats: %v", err)
	}
	return &pb.GetRateLimitStatsResponse{
		Allowed: stats.Allowed,
		Denied:  stats.Denied,
	}, nil
}
