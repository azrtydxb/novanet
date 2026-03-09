package ebpfservices

import (
	"context"
	"net"

	pb "github.com/azrtydxb/novanet/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AddMeshRedirect adds a mesh traffic redirection rule in the dataplane.
func (s *Server) AddMeshRedirect(ctx context.Context, req *pb.AddMeshRedirectRequest) (*pb.AddMeshRedirectResponse, error) {
	if req.Ip == "" {
		return nil, status.Errorf(codes.InvalidArgument, "ip is required")
	}
	if net.ParseIP(req.Ip) == nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid IP address: %s", req.Ip)
	}
	if req.Port == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "port is required")
	}
	if req.RedirectPort == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "redirect_port is required")
	}
	if s.dataplane == nil {
		return nil, status.Errorf(codes.Unavailable, "dataplane not connected")
	}
	if err := s.dataplane.UpsertMeshService(ctx, req.Ip, req.Port, req.RedirectPort); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to add mesh redirect: %v", err)
	}
	s.logger.Info("mesh redirect added",
		zap.String("ip", req.Ip),
		zap.Uint32("port", req.Port),
		zap.Uint32("redirect_port", req.RedirectPort))
	return &pb.AddMeshRedirectResponse{}, nil
}

// RemoveMeshRedirect removes a mesh traffic redirection rule from the dataplane.
func (s *Server) RemoveMeshRedirect(ctx context.Context, req *pb.RemoveMeshRedirectRequest) (*pb.RemoveMeshRedirectResponse, error) {
	if req.Ip == "" {
		return nil, status.Errorf(codes.InvalidArgument, "ip is required")
	}
	if net.ParseIP(req.Ip) == nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid IP address: %s", req.Ip)
	}
	if req.Port == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "port is required")
	}
	if s.dataplane == nil {
		return nil, status.Errorf(codes.Unavailable, "dataplane not connected")
	}
	if err := s.dataplane.DeleteMeshService(ctx, req.Ip, req.Port); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to remove mesh redirect: %v", err)
	}
	s.logger.Info("mesh redirect removed",
		zap.String("ip", req.Ip),
		zap.Uint32("port", req.Port))
	return &pb.RemoveMeshRedirectResponse{}, nil
}

// ListMeshRedirects lists all active mesh redirection rules from the dataplane.
func (s *Server) ListMeshRedirects(ctx context.Context, _ *pb.ListMeshRedirectsRequest) (*pb.ListMeshRedirectsResponse, error) {
	if s.dataplane == nil {
		return nil, status.Errorf(codes.Unavailable, "dataplane not connected")
	}
	entries, err := s.dataplane.ListMeshServices(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list mesh redirects: %v", err)
	}
	pbEntries := make([]*pb.MeshRedirectEntry, len(entries))
	for i, e := range entries {
		pbEntries[i] = &pb.MeshRedirectEntry{
			Ip:           e.IP,
			Port:         e.Port,
			RedirectPort: e.RedirectPort,
		}
	}
	return &pb.ListMeshRedirectsResponse{Entries: pbEntries}, nil
}
