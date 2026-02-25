package main

import (
	"context"
	"fmt"
	"time"

	pb "github.com/piwi3910/novanet/api/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// dialTimeout is the maximum time to wait for a gRPC connection.
	dialTimeout = 5 * time.Second

	// callTimeout is the maximum time to wait for a gRPC call response.
	callTimeout = 10 * time.Second
)

// connectAgent dials the agent gRPC socket and returns a client connection.
func connectAgent() (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx,
		"unix://"+agentSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to novanet-agent at %s: %w", agentSocket, err)
	}
	return conn, nil
}

// newAgentClient creates an AgentControl client from a gRPC connection.
func newAgentClient(conn *grpc.ClientConn) pb.AgentControlClient {
	return pb.NewAgentControlClient(conn)
}

// connectDataplane dials the dataplane gRPC socket and returns a client connection.
func connectDataplane() (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx,
		"unix://"+dataplaneSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to dataplane at %s: %w", dataplaneSocket, err)
	}
	return conn, nil
}
