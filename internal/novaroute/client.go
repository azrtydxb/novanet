// Package novaroute provides a gRPC client for communicating with the
// NovaRoute routing control plane via its Unix domain socket.
package novaroute

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RouteClient defines the interface for interacting with NovaRoute.
// This abstraction allows for testing with mock implementations.
type RouteClient interface {
	// Register registers this client with NovaRoute.
	Register(ctx context.Context, owner, token string) error
	// AdvertisePrefix advertises a route prefix to NovaRoute.
	AdvertisePrefix(ctx context.Context, owner, token, prefix, protocol string) error
	// WithdrawPrefix withdraws a previously advertised prefix.
	WithdrawPrefix(ctx context.Context, owner, token, prefix, protocol string) error
	// StreamEvents starts receiving route events from NovaRoute.
	StreamEvents(ctx context.Context, owner, token string, handler EventHandler) error
	// Close closes the underlying connection.
	Close() error
}

// EventHandler processes route events from NovaRoute.
type EventHandler func(event *RouteEvent)

// RouteEvent represents a routing event from NovaRoute.
type RouteEvent struct {
	Type     string // "add", "delete", "update"
	Prefix   string
	NextHop  string
	Protocol string
	Owner    string
}

// Client wraps a RouteClient with retry logic, registration, and lifecycle management.
type Client struct {
	mu sync.RWMutex

	socketPath string
	owner      string
	token      string
	logger     *zap.Logger

	routeClient RouteClient
	connected   atomic.Bool
	registered  atomic.Bool

	// eventHandler is called for each route event.
	eventHandler EventHandler

	// stopEvents cancels the event streaming goroutine.
	stopEvents context.CancelFunc

	// maxRetries is the maximum number of connect retries (0 = unlimited).
	maxRetries int

	// baseDelay is the base delay for exponential backoff.
	baseDelay time.Duration
	// maxDelay is the maximum delay for exponential backoff.
	maxDelay time.Duration
}

// NewClient creates a new NovaRoute client.
func NewClient(socketPath, owner, token string, logger *zap.Logger) *Client {
	return &Client{
		socketPath: socketPath,
		owner:      owner,
		token:      token,
		logger:     logger,
		baseDelay:  1 * time.Second,
		maxDelay:   30 * time.Second,
	}
}

// SetRouteClient overrides the default gRPC route client. Useful for testing.
func (c *Client) SetRouteClient(rc RouteClient) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routeClient = rc
}

// SetEventHandler sets the callback for route events.
func (c *Client) SetEventHandler(handler EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eventHandler = handler
}

// Connect establishes a connection to NovaRoute with retry logic.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.routeClient != nil {
		c.mu.Unlock()
		c.connected.Store(true)
		return nil
	}
	c.mu.Unlock()

	delay := c.baseDelay
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rc, err := newGRPCRouteClient(c.socketPath)
		if err == nil {
			c.mu.Lock()
			c.routeClient = rc
			c.mu.Unlock()
			c.connected.Store(true)
			c.logger.Info("connected to NovaRoute",
				zap.String("socket", c.socketPath),
			)
			return nil
		}

		attempt++
		if c.maxRetries > 0 && attempt >= c.maxRetries {
			return fmt.Errorf("failed to connect to NovaRoute after %d attempts: %w", attempt, err)
		}

		c.logger.Warn("failed to connect to NovaRoute, retrying",
			zap.Error(err),
			zap.Duration("delay", delay),
			zap.Int("attempt", attempt),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		// Exponential backoff.
		delay = delay * 2
		if delay > c.maxDelay {
			delay = c.maxDelay
		}
	}
}

// Register registers this agent with NovaRoute.
func (c *Client) Register(ctx context.Context) error {
	c.mu.RLock()
	rc := c.routeClient
	c.mu.RUnlock()

	if rc == nil {
		return fmt.Errorf("not connected to NovaRoute")
	}

	if err := rc.Register(ctx, c.owner, c.token); err != nil {
		return fmt.Errorf("registering with NovaRoute: %w", err)
	}

	c.registered.Store(true)
	c.logger.Info("registered with NovaRoute",
		zap.String("owner", c.owner),
	)
	return nil
}

// AdvertisePrefix advertises a route prefix to NovaRoute.
func (c *Client) AdvertisePrefix(ctx context.Context, prefix, protocol string) error {
	c.mu.RLock()
	rc := c.routeClient
	c.mu.RUnlock()

	if rc == nil {
		return fmt.Errorf("not connected to NovaRoute")
	}

	if err := rc.AdvertisePrefix(ctx, c.owner, c.token, prefix, protocol); err != nil {
		return fmt.Errorf("advertising prefix %s: %w", prefix, err)
	}

	c.logger.Info("advertised prefix",
		zap.String("prefix", prefix),
		zap.String("protocol", protocol),
	)
	return nil
}

// WithdrawPrefix withdraws a previously advertised prefix.
func (c *Client) WithdrawPrefix(ctx context.Context, prefix, protocol string) error {
	c.mu.RLock()
	rc := c.routeClient
	c.mu.RUnlock()

	if rc == nil {
		return fmt.Errorf("not connected to NovaRoute")
	}

	if err := rc.WithdrawPrefix(ctx, c.owner, c.token, prefix, protocol); err != nil {
		return fmt.Errorf("withdrawing prefix %s: %w", prefix, err)
	}

	c.logger.Info("withdrew prefix",
		zap.String("prefix", prefix),
		zap.String("protocol", protocol),
	)
	return nil
}

// StreamEvents starts streaming route events from NovaRoute in a background goroutine.
func (c *Client) StreamEvents(ctx context.Context) error {
	c.mu.RLock()
	rc := c.routeClient
	handler := c.eventHandler
	c.mu.RUnlock()

	if rc == nil {
		return fmt.Errorf("not connected to NovaRoute")
	}

	eventCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.stopEvents = cancel
	c.mu.Unlock()

	go func() {
		defer cancel()

		for {
			err := rc.StreamEvents(eventCtx, c.owner, c.token, handler)
			if err != nil {
				if eventCtx.Err() != nil {
					// Context was canceled, stop retrying.
					return
				}
				c.logger.Warn("event stream disconnected, reconnecting",
					zap.Error(err),
				)
				select {
				case <-eventCtx.Done():
					return
				case <-time.After(c.baseDelay):
				}
				continue
			}
			return
		}
	}()

	c.logger.Info("started event streaming from NovaRoute")
	return nil
}

// Close closes the connection to NovaRoute.
func (c *Client) Close() error {
	c.mu.Lock()
	rc := c.routeClient
	stopFn := c.stopEvents
	c.routeClient = nil
	c.stopEvents = nil
	c.mu.Unlock()

	if stopFn != nil {
		stopFn()
	}

	c.connected.Store(false)
	c.registered.Store(false)

	if rc == nil {
		return nil
	}

	if err := rc.Close(); err != nil {
		return fmt.Errorf("closing NovaRoute connection: %w", err)
	}

	c.logger.Info("closed NovaRoute connection")
	return nil
}

// IsConnected returns whether the client is connected to NovaRoute.
func (c *Client) IsConnected() bool {
	return c.connected.Load()
}

// IsRegistered returns whether the client has registered with NovaRoute.
func (c *Client) IsRegistered() bool {
	return c.registered.Load()
}

// grpcRouteClient is a real gRPC-based implementation of RouteClient.
// It connects to the NovaRoute service via a Unix domain socket and uses
// the raw gRPC interface to make RPC calls.
type grpcRouteClient struct {
	conn *grpc.ClientConn
}

// newGRPCRouteClient dials the NovaRoute Unix socket.
func newGRPCRouteClient(socketPath string) (*grpcRouteClient, error) {
	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dialing NovaRoute at %s: %w", socketPath, err)
	}

	return &grpcRouteClient{conn: conn}, nil
}

// Register calls the NovaRoute Register RPC.
// Since we don't have the generated proto types from NovaRoute, we use
// manual gRPC invocation with the known service/method names.
func (g *grpcRouteClient) Register(ctx context.Context, owner, token string) error {
	// Use raw gRPC invoke with empty request/response.
	// The actual NovaRoute proto would define RegisterRequest/RegisterResponse.
	// For now, we invoke the method and accept that it will fail until
	// the actual NovaRoute server is available.
	err := g.conn.Invoke(ctx, "/novaroute.v1.RouteControl/Register", &rawMessage{
		fields: map[string]string{
			"owner": owner,
			"token": token,
		},
	}, &rawMessage{})
	if err != nil {
		return fmt.Errorf("register RPC: %w", err)
	}
	return nil
}

// AdvertisePrefix calls the NovaRoute AdvertisePrefix RPC.
func (g *grpcRouteClient) AdvertisePrefix(ctx context.Context, owner, token, prefix, protocol string) error {
	err := g.conn.Invoke(ctx, "/novaroute.v1.RouteControl/AdvertisePrefix", &rawMessage{
		fields: map[string]string{
			"owner":    owner,
			"token":    token,
			"prefix":   prefix,
			"protocol": protocol,
		},
	}, &rawMessage{})
	if err != nil {
		return fmt.Errorf("advertise prefix RPC: %w", err)
	}
	return nil
}

// WithdrawPrefix calls the NovaRoute WithdrawPrefix RPC.
func (g *grpcRouteClient) WithdrawPrefix(ctx context.Context, owner, token, prefix, protocol string) error {
	err := g.conn.Invoke(ctx, "/novaroute.v1.RouteControl/WithdrawPrefix", &rawMessage{
		fields: map[string]string{
			"owner":    owner,
			"token":    token,
			"prefix":   prefix,
			"protocol": protocol,
		},
	}, &rawMessage{})
	if err != nil {
		return fmt.Errorf("withdraw prefix RPC: %w", err)
	}
	return nil
}

// StreamEvents would normally call a server-streaming RPC. Since we don't
// have the NovaRoute proto, this is a placeholder that blocks until context
// is canceled.
func (g *grpcRouteClient) StreamEvents(ctx context.Context, owner, token string, handler EventHandler) error {
	// Block until context is done. In the real implementation, this would
	// use a streaming RPC.
	<-ctx.Done()
	return ctx.Err()
}

// Close closes the gRPC connection.
func (g *grpcRouteClient) Close() error {
	return g.conn.Close()
}

// rawMessage is a minimal gRPC message that serializes to empty bytes.
// Used for RPCs where we don't have the generated proto types.
// In production, this would be replaced with actual NovaRoute proto types.
type rawMessage struct {
	fields map[string]string
}

func (r *rawMessage) Reset()         {}
func (r *rawMessage) String() string { return fmt.Sprintf("%v", r.fields) }
func (r *rawMessage) ProtoMessage()  {}
