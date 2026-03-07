package egressgateway

import (
	"net"
	"testing"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/azrtydxb/novanet/api/v1alpha1"
	"github.com/azrtydxb/novanet/internal/egress"
	"github.com/azrtydxb/novanet/internal/identity"
)

const testNodeA = "node-a"

func newTestReconciler(t *testing.T) (*Reconciler, *egress.Manager, *identity.Allocator) {
	t.Helper()

	logger := zap.NewNop()
	nodeIP := net.ParseIP("10.0.0.1")
	_, clusterCIDR, _ := net.ParseCIDR("10.244.0.0/16")

	egressMgr := egress.NewManager(nodeIP, clusterCIDR, logger)
	identityAlloc := identity.NewAllocator(logger)

	reconciler := NewReconciler(egressMgr, identityAlloc, logger)
	return reconciler, egressMgr, identityAlloc
}

func makePolicy(namespace, name string, destCIDRs, excludedCIDRs []string, egressIP string) *v1alpha1.EgressGatewayPolicy {
	return &v1alpha1.EgressGatewayPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1alpha1.EgressGatewayPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			DestinationCIDRs: destCIDRs,
			ExcludedCIDRs:    excludedCIDRs,
			GatewaySelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "egress-gateway"},
			},
			EgressIP: egressIP,
		},
	}
}

func TestReconcileSingleGatewayNode(t *testing.T) {
	r, egressMgr, identityAlloc := newTestReconciler(t)

	// Allocate an identity for the pod selector labels.
	identityAlloc.AllocateIdentity(map[string]string{"app": "web"})

	policy := makePolicy("default", "test-policy",
		[]string{"8.8.8.0/24", "1.1.1.0/24"}, nil, "192.168.1.100")

	nodes := []string{testNodeA}

	err := r.Reconcile(policy, nodes)
	if err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}

	// Check active gateway.
	gw, ok := r.GetActiveGateway("default", "test-policy")
	if !ok {
		t.Fatal("GetActiveGateway() returned false")
	}
	if gw != testNodeA {
		t.Errorf("active gateway = %q, want %q", gw, testNodeA)
	}

	// Check that egress rules were created.
	rules := egressMgr.GetRules()
	if len(rules) != 2 {
		t.Errorf("egress rules count = %d, want 2", len(rules))
	}

	// Verify policy is listed.
	policies := r.ListPolicies()
	if len(policies) != 1 {
		t.Fatalf("ListPolicies() returned %d, want 1", len(policies))
	}
	if policies[0].EgressIP != "192.168.1.100" {
		t.Errorf("egress IP = %q, want %q", policies[0].EgressIP, "192.168.1.100")
	}
}

func TestReconcileMultipleGatewayNodes(t *testing.T) {
	r, _, identityAlloc := newTestReconciler(t)

	identityAlloc.AllocateIdentity(map[string]string{"app": "web"})

	policy := makePolicy("default", "multi-gw",
		[]string{"8.8.8.0/24"}, nil, "")

	// Nodes in non-sorted order to verify deterministic selection.
	nodes := []string{"node-c", testNodeA, "node-b"}

	err := r.Reconcile(policy, nodes)
	if err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}

	// Active gateway should be the alphabetically first node.
	gw, ok := r.GetActiveGateway("default", "multi-gw")
	if !ok {
		t.Fatal("GetActiveGateway() returned false")
	}
	if gw != testNodeA {
		t.Errorf("active gateway = %q, want %q (alphabetically first)", gw, testNodeA)
	}

	// Verify all gateway nodes are stored sorted.
	policies := r.ListPolicies()
	if len(policies) != 1 {
		t.Fatalf("ListPolicies() returned %d, want 1", len(policies))
	}
	gwNodes := policies[0].GatewayNodes
	if len(gwNodes) != 3 {
		t.Fatalf("GatewayNodes count = %d, want 3", len(gwNodes))
	}
	if gwNodes[0] != testNodeA || gwNodes[1] != "node-b" || gwNodes[2] != "node-c" {
		t.Errorf("GatewayNodes = %v, want [node-a node-b node-c]", gwNodes)
	}

	// Reconcile again with same nodes in different order; result should be identical.
	err = r.Reconcile(policy, []string{"node-b", "node-c", testNodeA})
	if err != nil {
		t.Fatalf("second Reconcile() returned error: %v", err)
	}
	gw2, _ := r.GetActiveGateway("default", "multi-gw")
	if gw2 != testNodeA {
		t.Errorf("second reconcile: active gateway = %q, want %q", gw2, testNodeA)
	}
}

func TestDeleteCleansUpRules(t *testing.T) {
	r, egressMgr, identityAlloc := newTestReconciler(t)

	identityAlloc.AllocateIdentity(map[string]string{"app": "web"})

	policy := makePolicy("default", "delete-test",
		[]string{"8.8.8.0/24", "1.1.1.0/24"}, nil, "")

	err := r.Reconcile(policy, []string{testNodeA})
	if err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}

	// Verify rules exist.
	if egressMgr.Count() == 0 {
		t.Fatal("expected egress rules after reconcile")
	}

	// Delete the policy.
	err = r.Delete("default", "delete-test")
	if err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	// Verify rules are cleaned up.
	if egressMgr.Count() != 0 {
		t.Errorf("egress rules count after delete = %d, want 0", egressMgr.Count())
	}

	// Verify policy is no longer listed.
	_, ok := r.GetActiveGateway("default", "delete-test")
	if ok {
		t.Error("GetActiveGateway() returned true after delete")
	}

	policies := r.ListPolicies()
	if len(policies) != 0 {
		t.Errorf("ListPolicies() returned %d after delete, want 0", len(policies))
	}
}

func TestReconcileWithExcludedCIDRs(t *testing.T) {
	r, egressMgr, identityAlloc := newTestReconciler(t)

	identityAlloc.AllocateIdentity(map[string]string{"app": "web"})

	policy := makePolicy("kube-system", "excluded-test",
		[]string{"0.0.0.0/0"},
		[]string{"10.244.0.0/16", "10.96.0.0/12"},
		"203.0.113.5",
	)

	err := r.Reconcile(policy, []string{"gw-node-1"})
	if err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}

	// Verify rules were created for destination CIDRs.
	rules := egressMgr.GetRules()
	if len(rules) != 1 {
		t.Errorf("egress rules count = %d, want 1", len(rules))
	}

	// Verify excluded CIDRs are stored in the policy.
	policies := r.ListPolicies()
	if len(policies) != 1 {
		t.Fatalf("ListPolicies() returned %d, want 1", len(policies))
	}
	excluded := policies[0].ExcludedCIDRs
	if len(excluded) != 2 {
		t.Fatalf("ExcludedCIDRs count = %d, want 2", len(excluded))
	}
	if excluded[0] != "10.244.0.0/16" || excluded[1] != "10.96.0.0/12" {
		t.Errorf("ExcludedCIDRs = %v, want [10.244.0.0/16 10.96.0.0/12]", excluded)
	}

	// Verify egress IP.
	if policies[0].EgressIP != "203.0.113.5" {
		t.Errorf("EgressIP = %q, want %q", policies[0].EgressIP, "203.0.113.5")
	}
}

func TestReconcileNoNodesReturnsError(t *testing.T) {
	r, _, _ := newTestReconciler(t)

	policy := makePolicy("default", "no-nodes", []string{"8.8.8.0/24"}, nil, "")

	err := r.Reconcile(policy, nil)
	if err == nil {
		t.Fatal("Reconcile() with no nodes should return error")
	}
}

func TestReconcileInvalidCIDRReturnsError(t *testing.T) {
	r, _, _ := newTestReconciler(t)

	policy := makePolicy("default", "bad-cidr", []string{"not-a-cidr"}, nil, "")

	err := r.Reconcile(policy, []string{testNodeA})
	if err == nil {
		t.Fatal("Reconcile() with invalid CIDR should return error")
	}
}

func TestReconcileInvalidExcludedCIDRReturnsError(t *testing.T) {
	r, _, _ := newTestReconciler(t)

	policy := makePolicy("default", "bad-excluded",
		[]string{"8.8.8.0/24"}, []string{"invalid"}, "")

	err := r.Reconcile(policy, []string{testNodeA})
	if err == nil {
		t.Fatal("Reconcile() with invalid excluded CIDR should return error")
	}
}

func TestReconcileDefaultCIDR(t *testing.T) {
	r, egressMgr, identityAlloc := newTestReconciler(t)

	identityAlloc.AllocateIdentity(map[string]string{"app": "web"})

	// No destination CIDRs specified; should default to 0.0.0.0/0.
	policy := makePolicy("default", "default-cidr", nil, nil, "")

	err := r.Reconcile(policy, []string{testNodeA})
	if err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}

	rules := egressMgr.GetRules()
	if len(rules) != 1 {
		t.Fatalf("egress rules count = %d, want 1", len(rules))
	}

	if rules[0].DstCIDR.String() != "0.0.0.0/0" {
		t.Errorf("default CIDR = %q, want %q", rules[0].DstCIDR.String(), "0.0.0.0/0")
	}
}
