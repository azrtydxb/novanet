package ebpfservices

import (
	"context"
	"time"

	pb "github.com/azrtydxb/novanet/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// defaultPollInterval is the default polling interval for streaming health events.
	defaultPollInterval = 5 * time.Second
	// minPollInterval prevents overly aggressive polling.
	minPollInterval = 500 * time.Millisecond
)

// GetBackendHealth returns passive TCP health counters for backends.
func (s *Server) GetBackendHealth(ctx context.Context, req *pb.GetBackendHealthRequest) (*pb.GetBackendHealthResponse, error) {
	if s.dataplane == nil {
		return nil, status.Errorf(codes.Unavailable, "dataplane not connected")
	}
	backends, err := s.dataplane.GetBackendHealthStats(ctx, req.Ip, req.Port)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get backend health stats: %v", err)
	}
	pbBackends := make([]*pb.BackendHealthInfo, len(backends))
	for i, b := range backends {
		pbBackends[i] = &pb.BackendHealthInfo{
			Ip:           b.IP,
			Port:         b.Port,
			TotalConns:   b.TotalConns,
			FailedConns:  b.FailedConns,
			TimeoutConns: b.TimeoutConns,
			SuccessConns: b.SuccessConns,
			AvgRttNs:     b.AvgRTTNs,
			FailureRate:  b.FailureRate,
		}
	}
	return &pb.GetBackendHealthResponse{Backends: pbBackends}, nil
}

// StreamBackendHealth streams backend health events at the requested polling interval.
func (s *Server) StreamBackendHealth(req *pb.StreamBackendHealthRequest, stream grpc.ServerStreamingServer[pb.BackendHealthEvent]) error {
	if s.dataplane == nil {
		return status.Errorf(codes.Unavailable, "dataplane not connected")
	}

	interval := defaultPollInterval
	if req.PollIntervalMs > 0 {
		interval = time.Duration(req.PollIntervalMs) * time.Millisecond
		if interval < minPollInterval {
			interval = minPollInterval
		}
	}

	s.logger.Info("starting backend health stream",
		zap.Duration("poll_interval", interval))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			backends, err := s.dataplane.GetBackendHealthStats(ctx, "", 0)
			if err != nil {
				s.logger.Warn("failed to poll backend health stats", zap.Error(err))
				continue
			}
			now := time.Now().UnixNano()
			for _, b := range backends {
				event := &pb.BackendHealthEvent{
					Backend: &pb.BackendHealthInfo{
						Ip:           b.IP,
						Port:         b.Port,
						TotalConns:   b.TotalConns,
						FailedConns:  b.FailedConns,
						TimeoutConns: b.TimeoutConns,
						SuccessConns: b.SuccessConns,
						AvgRttNs:     b.AvgRTTNs,
						FailureRate:  b.FailureRate,
					},
					TimestampNs: uint64(now), //nolint:gosec // timestamp always positive
				}
				if err := stream.Send(event); err != nil {
					return err
				}
			}
		}
	}
}
