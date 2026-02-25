// Package node implements a thread-safe registry for tracking Kubernetes
// cluster nodes, their IPs, and PodCIDR assignments.
package node

import (
	"sort"
	"sync"

	"go.uber.org/zap"
)

// NodeInfo holds information about a cluster node.
type NodeInfo struct {
	// Name is the Kubernetes node name.
	Name string
	// IP is the node's internal IP address.
	IP string
	// PodCIDR is the CIDR block assigned to this node for pod networking.
	PodCIDR string
	// Ready indicates whether the node is in Ready condition.
	Ready bool
}

// ChangeCallback is called when a node is added, updated, or removed.
type ChangeCallback func(event string, node *NodeInfo)

// Registry tracks cluster nodes and notifies listeners on changes.
type Registry struct {
	mu sync.RWMutex

	logger    *zap.Logger
	nodes     map[string]*NodeInfo
	callbacks []ChangeCallback
}

// NewRegistry creates a new node registry.
func NewRegistry(logger *zap.Logger) *Registry {
	return &Registry{
		logger: logger,
		nodes:  make(map[string]*NodeInfo),
	}
}

// AddNode adds or updates a node in the registry.
func (r *Registry) AddNode(name, ip, podCIDR string) {
	r.mu.Lock()

	existing, isUpdate := r.nodes[name]
	node := &NodeInfo{
		Name:    name,
		IP:      ip,
		PodCIDR: podCIDR,
		Ready:   true,
	}
	r.nodes[name] = node

	// Copy callbacks while holding the lock.
	callbacks := make([]ChangeCallback, len(r.callbacks))
	copy(callbacks, r.callbacks)
	r.mu.Unlock()

	event := "add"
	if isUpdate {
		event = "update"
		r.logger.Info("updated node",
			zap.String("node", name),
			zap.String("ip", ip),
			zap.String("pod_cidr", podCIDR),
			zap.String("old_ip", existing.IP),
		)
	} else {
		r.logger.Info("added node",
			zap.String("node", name),
			zap.String("ip", ip),
			zap.String("pod_cidr", podCIDR),
		)
	}

	for _, cb := range callbacks {
		cb(event, node)
	}
}

// RemoveNode removes a node from the registry.
func (r *Registry) RemoveNode(name string) {
	r.mu.Lock()

	node, ok := r.nodes[name]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.nodes, name)

	callbacks := make([]ChangeCallback, len(r.callbacks))
	copy(callbacks, r.callbacks)
	r.mu.Unlock()

	r.logger.Info("removed node",
		zap.String("node", name),
	)

	for _, cb := range callbacks {
		cb("delete", node)
	}
}

// GetNode returns information about a specific node.
func (r *Registry) GetNode(name string) (*NodeInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	node, ok := r.nodes[name]
	if !ok {
		return nil, false
	}

	// Return a copy.
	result := *node
	return &result, true
}

// ListNodes returns a snapshot of all nodes, sorted by name.
func (r *Registry) ListNodes() []*NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*NodeInfo, 0, len(r.nodes))
	for _, node := range r.nodes {
		n := *node
		result = append(result, &n)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// OnNodeChange registers a callback that is invoked on any node change.
// The callback receives the event type ("add", "update", "delete") and
// the affected node.
func (r *Registry) OnNodeChange(cb ChangeCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbacks = append(r.callbacks, cb)
}

// SetReady updates the ready status of a node.
func (r *Registry) SetReady(name string, ready bool) {
	r.mu.Lock()

	node, ok := r.nodes[name]
	if !ok {
		r.mu.Unlock()
		return
	}
	node.Ready = ready

	nodeCopy := *node
	callbacks := make([]ChangeCallback, len(r.callbacks))
	copy(callbacks, r.callbacks)
	r.mu.Unlock()

	r.logger.Info("node readiness changed",
		zap.String("node", name),
		zap.Bool("ready", ready),
	)

	for _, cb := range callbacks {
		cb("update", &nodeCopy)
	}
}

// Count returns the number of tracked nodes.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}
