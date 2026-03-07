// Package egressgateway implements the reconciler for EgressGatewayPolicy CRDs.
// It watches policies, selects active gateway nodes, and pushes egress rules
// to the egress manager.
package egressgateway

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/azrtydxb/novanet/api/v1alpha1"
	"github.com/azrtydxb/novanet/internal/egress"
	"github.com/azrtydxb/novanet/internal/identity"
)

// ErrNoGatewayNodes is returned when no gateway nodes are available for an egress policy.
var ErrNoGatewayNodes = errors.New("no gateway nodes available for policy")

// Policy holds the reconciled state of an EgressGatewayPolicy.
type Policy struct {
	Namespace     string
	Name          string
	PodSelector   metav1.LabelSelector
	DestCIDRs     []string
	ExcludedCIDRs []string
	GatewayNodes  []string
	ActiveGateway string
	EgressIP      string
}

// Reconciler watches EgressGatewayPolicy CRDs and manages the corresponding
// egress rules in the egress manager.
type Reconciler struct {
	egressMgr     *egress.Manager
	identityAlloc *identity.Allocator
	logger        *zap.Logger
	mu            sync.RWMutex
	policies      map[string]*Policy // key: namespace/name
}

// NewReconciler creates a new egress gateway reconciler.
func NewReconciler(egressMgr *egress.Manager, identityAlloc *identity.Allocator, logger *zap.Logger) *Reconciler {
	return &Reconciler{
		egressMgr:     egressMgr,
		identityAlloc: identityAlloc,
		logger:        logger,
		policies:      make(map[string]*Policy),
	}
}

// Reconcile processes an EgressGatewayPolicy and updates the egress rules.
// The nodes parameter is the list of node names matching the GatewaySelector.
func (r *Reconciler) Reconcile(policy *v1alpha1.EgressGatewayPolicy, nodes []string) error {
	namespace := policy.Namespace
	name := policy.Name
	key := policyKey(namespace, name)

	r.logger.Info("reconciling egress gateway policy",
		zap.String("namespace", namespace),
		zap.String("name", name),
		zap.Int("gateway_nodes", len(nodes)),
	)

	if len(nodes) == 0 {
		return fmt.Errorf("%w: %s", ErrNoGatewayNodes, key)
	}

	// Validate destination CIDRs.
	destCIDRs := policy.Spec.DestinationCIDRs
	for _, cidr := range destCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid destination CIDR %q: %w", cidr, err)
		}
	}

	// Validate excluded CIDRs.
	for _, cidr := range policy.Spec.ExcludedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid excluded CIDR %q: %w", cidr, err)
		}
	}

	// Deterministic gateway selection: sort alphabetically, pick first.
	sortedNodes := make([]string, len(nodes))
	copy(sortedNodes, nodes)
	sort.Strings(sortedNodes)
	activeGateway := sortedNodes[0]

	// Determine egress IP: use spec if set, otherwise empty (gateway node IP).
	egressIP := policy.Spec.EgressIP

	// Find matching source identities for the pod selector.
	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
	if err != nil {
		return fmt.Errorf("parsing pod selector: %w", err)
	}
	matchingIdentities := r.identityAlloc.FindMatchingIdentities(selector)

	// Remove old rules for this policy before adding new ones.
	r.removeRulesForPolicy(namespace, name)

	// If no destination CIDRs specified, use a default catch-all 0.0.0.0/0.
	if len(destCIDRs) == 0 {
		destCIDRs = []string{"0.0.0.0/0"}
	}

	// Add egress rules for each destination CIDR and matching identity.
	for i, cidr := range destCIDRs {
		for j, srcID := range matchingIdentities {
			ruleName := fmt.Sprintf("egp-%s-%s-%d-%d", name, activeGateway, i, j)
			rule := egress.Rule{
				Name:        ruleName,
				SrcIdentity: srcID,
				DstCIDR:     cidr,
				Protocol:    0, // any
				DstPort:     0, // any
				Action:      egress.ActionSNAT,
			}
			if err := r.egressMgr.AddEgressRule(namespace, rule); err != nil {
				return fmt.Errorf("adding egress rule for CIDR %s: %w", cidr, err)
			}
		}
	}

	// If there are no matching identities but we still have CIDRs, add rules
	// with identity 0 (wildcard) so they are present for when pods appear.
	if len(matchingIdentities) == 0 {
		for i, cidr := range destCIDRs {
			ruleName := fmt.Sprintf("egp-%s-%s-%d", name, activeGateway, i)
			rule := egress.Rule{
				Name:        ruleName,
				SrcIdentity: 0,
				DstCIDR:     cidr,
				Protocol:    0,
				DstPort:     0,
				Action:      egress.ActionSNAT,
			}
			if err := r.egressMgr.AddEgressRule(namespace, rule); err != nil {
				return fmt.Errorf("adding egress rule for CIDR %s: %w", cidr, err)
			}
		}
	}

	// Store the policy state.
	r.mu.Lock()
	r.policies[key] = &Policy{
		Namespace:     namespace,
		Name:          name,
		PodSelector:   policy.Spec.PodSelector,
		DestCIDRs:     destCIDRs,
		ExcludedCIDRs: policy.Spec.ExcludedCIDRs,
		GatewayNodes:  sortedNodes,
		ActiveGateway: activeGateway,
		EgressIP:      egressIP,
	}
	r.mu.Unlock()

	r.logger.Info("reconciled egress gateway policy",
		zap.String("namespace", namespace),
		zap.String("name", name),
		zap.String("active_gateway", activeGateway),
		zap.String("egress_ip", egressIP),
		zap.Int("dest_cidrs", len(destCIDRs)),
	)

	return nil
}

// Delete removes all egress rules associated with the named policy.
func (r *Reconciler) Delete(namespace, name string) error {
	key := policyKey(namespace, name)

	r.logger.Info("deleting egress gateway policy",
		zap.String("namespace", namespace),
		zap.String("name", name),
	)

	r.removeRulesForPolicy(namespace, name)

	r.mu.Lock()
	delete(r.policies, key)
	r.mu.Unlock()

	return nil
}

// GetActiveGateway returns the active gateway node for the named policy.
func (r *Reconciler) GetActiveGateway(namespace, name string) (string, bool) {
	key := policyKey(namespace, name)

	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.policies[key]
	if !ok {
		return "", false
	}
	return p.ActiveGateway, true
}

// ListPolicies returns a snapshot of all tracked policies.
func (r *Reconciler) ListPolicies() []Policy {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Policy, 0, len(r.policies))
	for _, p := range r.policies {
		result = append(result, *p)
	}
	return result
}

// removeRulesForPolicy removes all egress manager rules that belong to the
// given policy. It uses the naming convention "egp-<name>-" to identify rules.
func (r *Reconciler) removeRulesForPolicy(namespace, name string) {
	prefix := fmt.Sprintf("egp-%s-", name)
	rules := r.egressMgr.GetRules()
	for _, rule := range rules {
		if rule.Namespace == namespace && len(rule.Name) >= len(prefix) && rule.Name[:len(prefix)] == prefix {
			r.egressMgr.RemoveEgressRule(namespace, rule.Name)
		}
	}
}

func policyKey(namespace, name string) string {
	return namespace + "/" + name
}
