// Package ebpfservices implements the EBPFServices gRPC server for managing
// kernel-level eBPF operations including sockmap acceleration, mesh traffic
// redirection, rate limiting, and backend health monitoring.
package ebpfservices

import (
	pb "github.com/azrtydxb/novanet/api/v1"
	"github.com/azrtydxb/novanet/internal/dataplane"
	"go.uber.org/zap"
)

// Server implements the EBPFServices gRPC service.
type Server struct {
	pb.UnimplementedEBPFServicesServer
	logger    *zap.Logger
	dataplane dataplane.ClientInterface
}

// NewServer creates a new EBPFServices server.
// The dataplane client may be nil if the dataplane is not connected;
// RPCs that require it will return codes.Unavailable.
func NewServer(logger *zap.Logger, dp dataplane.ClientInterface) *Server {
	return &Server{logger: logger, dataplane: dp}
}
