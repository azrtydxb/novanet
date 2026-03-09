// Package routing provides an integrated routing manager that replaces the
// former NovaRoute gRPC client. It manages BGP/BFD/OSPF routing via FRR
// directly in-process using an intent store and reconciler.
package routing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/azrtydxb/novanet/internal/routing/frr"
	"github.com/azrtydxb/novanet/internal/routing/intent"
	"github.com/azrtydxb/novanet/internal/routing/reconciler"
	rtypes "github.com/azrtydxb/novanet/internal/routing/types"
	"go.uber.org/zap"
)

// ManagerConfig holds configuration for the routing manager.
type ManagerConfig struct {
	// FRRSocketDir is the directory where FRR VTY sockets are located.
	FRRSocketDir string
	// ReconcileInterval is the periodic reconciliation interval.
	ReconcileInterval time.Duration
}

// RouteEvent represents a routing event for streaming to subscribers.
type RouteEvent struct {
	EventType string
	Owner     string
	Detail    string
	Metadata  map[string]string
}

// Manager provides the routing control interface, replacing the former
// NovaRoute gRPC client. It manages intents and reconciles them to FRR
// configuration via the vtysh client.
type Manager struct {
	logger *zap.Logger

	store      *intent.Store
	rec        *reconciler.Reconciler
	frrClient  *frr.Client
	owner      string
	reconcileI time.Duration

	cancel context.CancelFunc
	done   chan struct{}

	// Event subscription for streaming routing events.
	subMu       sync.Mutex
	subscribers map[chan RouteEvent]struct{}
}

// NewManager creates a new routing manager.
func NewManager(cfg ManagerConfig, owner string, logger *zap.Logger) *Manager {
	if cfg.FRRSocketDir == "" {
		cfg.FRRSocketDir = "/run/frr"
	}
	if cfg.ReconcileInterval == 0 {
		cfg.ReconcileInterval = 30 * time.Second
	}

	store := intent.NewStore(logger)
	frrClient := frr.NewClient(cfg.FRRSocketDir, logger)
	rec := reconciler.NewReconciler(store, frrClient, logger, nil)

	mgr := &Manager{
		logger:      logger.With(zap.String("component", "routing")),
		store:       store,
		rec:         rec,
		frrClient:   frrClient,
		owner:       owner,
		reconcileI:  cfg.ReconcileInterval,
		done:        make(chan struct{}),
		subscribers: make(map[chan RouteEvent]struct{}),
	}

	// Wire the manager as the event publisher for the reconciler.
	rec.SetEventPublisher(mgr)

	return mgr
}

// Start begins the periodic reconciliation loop. The FRR client connects
// lazily on first vtysh command, so no explicit connect is needed.
func (m *Manager) Start(ctx context.Context) {
	childCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	go func() {
		defer close(m.done)
		m.rec.RunLoop(childCtx, m.reconcileI)
	}()

	m.logger.Info("routing manager started",
		zap.Duration("reconcile_interval", m.reconcileI))
}

// ErrFRRNotReady is returned when FRR daemons are not available after retries.
var ErrFRRNotReady = errors.New("FRR daemons not ready")

// WaitForFRR blocks until FRR daemon sockets are available, with retries.
func (m *Manager) WaitForFRR(ctx context.Context) error {
	const maxRetries = 30
	delay := time.Second

	for attempt := 1; ; attempt++ {
		if m.frrClient.IsReady() {
			m.logger.Info("FRR daemons ready")
			return nil
		}

		if attempt >= maxRetries {
			return fmt.Errorf("%w after %d attempts", ErrFRRNotReady, attempt)
		}

		m.logger.Info("waiting for FRR daemons...", zap.Int("attempt", attempt))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// ConfigureBGP sets the BGP global configuration (AS number and router ID).
func (m *Manager) ConfigureBGP(localAS uint32, routerID string) {
	m.rec.UpdateBGPGlobal(localAS, routerID)
	m.rec.TriggerReconcile()
	m.logger.Info("BGP configured",
		zap.Uint32("local_as", localAS),
		zap.String("router_id", routerID))
}

// BFDOptions holds BFD configuration for a BGP peer.
type BFDOptions struct {
	Enabled          bool
	MinRxMs          uint32
	MinTxMs          uint32
	DetectMultiplier uint32
}

// ApplyPeer adds or updates a BGP peer with optional BFD configuration.
func (m *Manager) ApplyPeer(neighborAddr string, remoteAS uint32, bfd *BFDOptions) error {
	peer := intent.PeerIntent{
		Owner:           m.owner,
		NeighborAddress: neighborAddr,
		RemoteAS:        remoteAS,
		PeerType:        rtypes.PeerTypeExternal,
		AddressFamilies: []rtypes.AddressFamily{rtypes.AddressFamilyIPv4Unicast},
	}
	if bfd != nil && bfd.Enabled {
		peer.BFDEnabled = true
		peer.BFDMinRxMs = bfd.MinRxMs
		peer.BFDMinTxMs = bfd.MinTxMs
		peer.BFDDetectMultiplier = bfd.DetectMultiplier
	}

	if err := m.store.SetPeerIntent(m.owner, &peer); err != nil {
		return fmt.Errorf("setting peer intent for %s: %w", neighborAddr, err)
	}
	m.rec.TriggerReconcile()

	m.logger.Info("applied BGP peer",
		zap.String("neighbor", neighborAddr),
		zap.Uint32("remote_as", remoteAS),
		zap.Bool("bfd", bfd != nil && bfd.Enabled))
	return nil
}

// AdvertisePrefix advertises a route prefix via BGP.
func (m *Manager) AdvertisePrefix(prefix string) error {
	pfx := intent.PrefixIntent{
		Owner:    m.owner,
		Prefix:   prefix,
		Protocol: rtypes.ProtocolBGP,
	}

	if err := m.store.SetPrefixIntent(m.owner, &pfx); err != nil {
		return fmt.Errorf("setting prefix intent for %s: %w", prefix, err)
	}
	m.rec.TriggerReconcile()

	m.logger.Info("advertised prefix", zap.String("prefix", prefix))
	return nil
}

// WithdrawPrefix withdraws a previously advertised prefix.
func (m *Manager) WithdrawPrefix(prefix string) error {
	if err := m.store.RemovePrefixIntent(m.owner, prefix, "bgp"); err != nil {
		m.logger.Debug("prefix not found for withdrawal (may already be removed)",
			zap.String("prefix", prefix), zap.Error(err))
		return nil
	}
	m.rec.TriggerReconcile()

	m.logger.Info("withdrew prefix", zap.String("prefix", prefix))
	return nil
}

// Store returns the underlying intent store for direct access (e.g. by CLI).
func (m *Manager) Store() *intent.Store {
	return m.store
}

// FRRClient returns the underlying FRR client for direct vtysh queries (e.g. by CLI).
func (m *Manager) FRRClient() *frr.Client {
	return m.frrClient
}

// RemovePeer removes a BGP peer by neighbor address.
func (m *Manager) RemovePeer(neighborAddr string) error {
	if err := m.store.RemovePeerIntent(m.owner, neighborAddr); err != nil {
		return fmt.Errorf("removing peer intent for %s: %w", neighborAddr, err)
	}
	m.rec.TriggerReconcile()
	m.logger.Info("removed BGP peer", zap.String("neighbor", neighborAddr))
	return nil
}

// EnableBFD enables BFD on a peer with the given timers.
func (m *Manager) EnableBFD(peerAddr string, minRxMs, minTxMs, detectMult uint32, iface string) error {
	bfd := intent.BFDIntent{
		Owner:            m.owner,
		PeerAddress:      peerAddr,
		MinRxMs:          minRxMs,
		MinTxMs:          minTxMs,
		DetectMultiplier: detectMult,
		InterfaceName:    iface,
	}
	if err := m.store.SetBFDIntent(m.owner, &bfd); err != nil {
		return fmt.Errorf("setting BFD intent for %s: %w", peerAddr, err)
	}
	m.rec.TriggerReconcile()
	m.logger.Info("enabled BFD", zap.String("peer", peerAddr))
	return nil
}

// DisableBFD disables BFD on a peer.
func (m *Manager) DisableBFD(peerAddr string) error {
	if err := m.store.RemoveBFDIntent(m.owner, peerAddr); err != nil {
		m.logger.Debug("BFD intent not found for removal",
			zap.String("peer", peerAddr), zap.Error(err))
		return nil
	}
	m.rec.TriggerReconcile()
	m.logger.Info("disabled BFD", zap.String("peer", peerAddr))
	return nil
}

// EnableOSPF enables OSPF on an interface.
func (m *Manager) EnableOSPF(ifaceName, areaID string, passive bool, cost, hello, dead uint32, ipv6 bool) error {
	ospf := intent.OSPFIntent{
		Owner:         m.owner,
		InterfaceName: ifaceName,
		AreaID:        areaID,
		Passive:       passive,
		Cost:          cost,
		HelloInterval: hello,
		DeadInterval:  dead,
		IPv6:          ipv6,
	}
	if err := m.store.SetOSPFIntent(m.owner, &ospf); err != nil {
		return fmt.Errorf("setting OSPF intent for %s: %w", ifaceName, err)
	}
	m.rec.TriggerReconcile()
	m.logger.Info("enabled OSPF", zap.String("interface", ifaceName), zap.String("area", areaID))
	return nil
}

// DisableOSPF disables OSPF on an interface.
func (m *Manager) DisableOSPF(ifaceName string) error {
	if err := m.store.RemoveOSPFIntent(m.owner, ifaceName); err != nil {
		m.logger.Debug("OSPF intent not found for removal",
			zap.String("interface", ifaceName), zap.Error(err))
		return nil
	}
	m.rec.TriggerReconcile()
	m.logger.Info("disabled OSPF", zap.String("interface", ifaceName))
	return nil
}

// --- Event subscription ---

// eventTypeNames maps event type IDs to human-readable names.
var eventTypeNames = map[uint32]string{
	uint32(rtypes.EventTypePeerUp):            "peer_up",
	uint32(rtypes.EventTypePeerDown):          "peer_down",
	uint32(rtypes.EventTypePrefixAdvertised):  "prefix_advertised",
	uint32(rtypes.EventTypePrefixWithdrawn):   "prefix_withdrawn",
	uint32(rtypes.EventTypeBFDUp):             "bfd_up",
	uint32(rtypes.EventTypeBFDDown):           "bfd_down",
	uint32(rtypes.EventTypeOSPFNeighborUp):    "ospf_neighbor_up",
	uint32(rtypes.EventTypeOSPFNeighborDown):  "ospf_neighbor_down",
	uint32(rtypes.EventTypeFRRConnected):      "frr_connected",
	uint32(rtypes.EventTypeFRRDisconnected):   "frr_disconnected",
	uint32(rtypes.EventTypeOwnerRegistered):   "owner_registered",
	uint32(rtypes.EventTypeOwnerDeregistered): "owner_deregistered",
	uint32(rtypes.EventTypePolicyViolation):   "policy_violation",
	uint32(rtypes.EventTypeBGPConfigChanged):  "bgp_config_changed",
}

// PublishRouteEvent implements the reconciler.EventPublisher interface.
func (m *Manager) PublishRouteEvent(eventType uint32, owner, detail string, metadata map[string]string) {
	name := eventTypeNames[eventType]
	if name == "" {
		name = fmt.Sprintf("unknown_%d", eventType)
	}

	evt := RouteEvent{
		EventType: name,
		Owner:     owner,
		Detail:    detail,
		Metadata:  metadata,
	}

	m.subMu.Lock()
	defer m.subMu.Unlock()

	for ch := range m.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop event for slow subscribers.
		}
	}
}

// SubscribeEvents returns a channel that receives routing events.
func (m *Manager) SubscribeEvents() chan RouteEvent {
	ch := make(chan RouteEvent, 256)

	m.subMu.Lock()
	m.subscribers[ch] = struct{}{}
	m.subMu.Unlock()

	return ch
}

// UnsubscribeEvents removes a subscriber channel and closes it.
func (m *Manager) UnsubscribeEvents(ch chan RouteEvent) {
	m.subMu.Lock()
	delete(m.subscribers, ch)
	m.subMu.Unlock()

	close(ch)
}

// Shutdown stops the reconciler and disconnects from FRR.
func (m *Manager) Shutdown() {
	if m.cancel != nil {
		m.cancel()
		<-m.done
	}
	_ = m.frrClient.Close()

	// Close all subscriber channels.
	m.subMu.Lock()
	for ch := range m.subscribers {
		close(ch)
		delete(m.subscribers, ch)
	}
	m.subMu.Unlock()

	m.logger.Info("routing manager stopped")
}
