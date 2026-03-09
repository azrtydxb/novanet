package ebpfservices

import (
	"context"

	pb "github.com/azrtydxb/novanet/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// EnableSockmap enables sockmap-based acceleration for a pod pair.
// Currently logs the request and returns success. Full implementation
// requires resolving the pod IP from the endpoint store, which will be
// added when the endpoint store is integrated.
func (s *Server) EnableSockmap(_ context.Context, req *pb.EnableSockmapRequest) (*pb.EnableSockmapResponse, error) {
	if req.PodNamespace == "" {
		return nil, status.Errorf(codes.InvalidArgument, "pod_namespace is required")
	}
	if req.PodName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "pod_name is required")
	}
	s.logger.Info("EnableSockmap requested",
		zap.String("namespace", req.PodNamespace),
		zap.String("name", req.PodName))
	// TODO: resolve pod IP from endpoint store, then call s.dataplane.UpsertSockmapEndpoint
	return &pb.EnableSockmapResponse{}, nil
}

// DisableSockmap disables sockmap-based acceleration for a pod pair.
// Currently logs the request and returns success. Full implementation
// requires resolving the pod IP from the endpoint store.
func (s *Server) DisableSockmap(_ context.Context, req *pb.DisableSockmapRequest) (*pb.DisableSockmapResponse, error) {
	if req.PodNamespace == "" {
		return nil, status.Errorf(codes.InvalidArgument, "pod_namespace is required")
	}
	if req.PodName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "pod_name is required")
	}
	s.logger.Info("DisableSockmap requested",
		zap.String("namespace", req.PodNamespace),
		zap.String("name", req.PodName))
	// TODO: resolve pod IP from endpoint store, then call s.dataplane.DeleteSockmapEndpoint
	return &pb.DisableSockmapResponse{}, nil
}

// GetSockmapStats returns sockmap statistics from the dataplane.
func (s *Server) GetSockmapStats(ctx context.Context, _ *pb.GetSockmapStatsRequest) (*pb.GetSockmapStatsResponse, error) {
	if s.dataplane == nil {
		return nil, status.Errorf(codes.Unavailable, "dataplane not connected")
	}
	stats, err := s.dataplane.GetSockmapStats(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get sockmap stats: %v", err)
	}
	return &pb.GetSockmapStatsResponse{
		Redirected:    stats.Redirected,
		Fallback:      stats.Fallback,
		ActiveSockets: stats.ActiveEndpoints,
	}, nil
}
