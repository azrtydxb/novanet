package ebpfservices

import (
	"context"
	"testing"

	pb "github.com/azrtydxb/novanet/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func TestNewServer(t *testing.T) {
	s := NewServer(testLogger())
	if s == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestServerImplementsInterface(t *testing.T) {
	s := NewServer(testLogger())
	// Verify the server satisfies the interface.
	var _ pb.EBPFServicesServer = s
}

func TestUnimplementedRPCs(t *testing.T) {
	s := NewServer(testLogger())
	ctx := context.Background()

	// Test EnableSockmap returns Unimplemented.
	_, err := s.EnableSockmap(ctx, &pb.EnableSockmapRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unimplemented {
		t.Fatalf("expected Unimplemented, got %v", err)
	}
}
