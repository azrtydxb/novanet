package policy

import (
	"context"
	"net"
	"testing"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/azrtydxb/novanet/api/v1alpha1"
	"github.com/azrtydxb/novanet/internal/identity"
)

func testExtendedCompiler() *ExtendedCompiler {
	logger, _ := zap.NewDevelopment()
	idAlloc := identity.NewAllocator(logger)
	return NewExtendedCompiler(idAlloc, logger)
}

func mockDNSCache(logger *zap.Logger) *DNSCache {
	cache := NewDNSCache(logger, defaultMaxEntries)
	cache.SetResolver(func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "api.example.com":
			return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("93.184.216.35")}, nil
		case "db.internal":
			return []net.IP{net.ParseIP("10.0.0.50")}, nil
		}
		return nil, &net.DNSError{Err: "not found", Name: host}
	})
	return cache
}

func TestExtendedCompileNil(t *testing.T) {
	c := testExtendedCompiler()
	rules := c.CompilePolicy(nil)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules for nil policy, got %d", len(rules))
	}
}

func TestExtendedCompilePortRange(t *testing.T) {
	c := testExtendedCompiler()

	proto := protocolStrTCP
	port := int32(8080)
	endPort := int32(8085)

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "port-range",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeIngress},
			Ingress: []v1alpha1.NovaNetworkPolicyIngressRule{
				{
					Ports: []v1alpha1.NovaNetworkPolicyPort{
						{Protocol: &proto, Port: &port, EndPort: &endPort},
					},
				},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	// 6 ports (8080-8085) x 1 source (wildcard) = 6 rules.
	if len(rules) != 6 {
		t.Fatalf("expected 6 rules for port range 8080-8085, got %d", len(rules))
	}

	// Verify all ports in range are present.
	portMap := make(map[uint16]bool)
	for _, r := range rules {
		portMap[r.DstPort] = true
		if r.Protocol != ProtocolTCP {
			t.Fatalf("expected TCP protocol, got %d", r.Protocol)
		}
		if r.Action != ActionAllow {
			t.Fatalf("expected allow action, got %d", r.Action)
		}
	}
	for p := uint16(8080); p <= 8085; p++ {
		if !portMap[p] {
			t.Fatalf("missing rule for port %d", p)
		}
	}
}

func TestExtendedCompileLargePortRange(t *testing.T) {
	c := testExtendedCompiler()

	proto := protocolStrTCP
	port := int32(1000)
	endPort := int32(2000)

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "large-range",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeIngress},
			Ingress: []v1alpha1.NovaNetworkPolicyIngressRule{
				{
					Ports: []v1alpha1.NovaNetworkPolicyPort{
						{Protocol: &proto, Port: &port, EndPort: &endPort},
					},
				},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	// Large range: should produce 1 rule with DstPort=0 (wildcard).
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule for large port range, got %d", len(rules))
	}
	if rules[0].DstPort != 0 {
		t.Fatalf("expected wildcard port (0) for large range, got %d", rules[0].DstPort)
	}
}

func TestExtendedCompileCIDRPeers(t *testing.T) {
	c := testExtendedCompiler()

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cidr-policy",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeIngress},
			Ingress: []v1alpha1.NovaNetworkPolicyIngressRule{
				{
					From: []v1alpha1.NovaNetworkPolicyPeer{
						{
							IPBlock: &v1alpha1.NovaIPBlock{
								CIDR:   "10.0.0.0/8",
								Except: []string{"10.0.1.0/24"},
							},
						},
					},
				},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	// 1 allow CIDR + 1 deny except = 2 rules.
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (allow + except deny), got %d", len(rules))
	}

	foundAllow := false
	foundDeny := false
	for _, r := range rules {
		if r.CIDR == "10.0.0.0/8" && r.Action == ActionAllow {
			foundAllow = true
		}
		if r.CIDR == "10.0.1.0/24" && r.Action == ActionDeny {
			foundDeny = true
		}
	}
	if !foundAllow {
		t.Fatal("missing allow CIDR rule for 10.0.0.0/8")
	}
	if !foundDeny {
		t.Fatal("missing deny CIDR rule for 10.0.1.0/24")
	}
}

func TestExtendedCompileFQDNPeers(t *testing.T) {
	c := testExtendedCompiler()
	logger, _ := zap.NewDevelopment()
	c.SetDNSCache(mockDNSCache(logger))

	fqdn := "api.example.com"
	proto := protocolStrTCP
	port := int32(443)

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fqdn-policy",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeEgress},
			Egress: []v1alpha1.NovaNetworkPolicyEgressRule{
				{
					To: []v1alpha1.NovaNetworkPolicyPeer{
						{FQDN: &fqdn},
					},
					Ports: []v1alpha1.NovaNetworkPolicyPort{
						{Protocol: &proto, Port: &port},
					},
				},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	// api.example.com resolves to 2 IPs -> 2 CIDR rules.
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (one per resolved IP), got %d", len(rules))
	}

	for _, r := range rules {
		if r.Action != ActionAllow {
			t.Fatalf("expected allow action, got %d", r.Action)
		}
		if !r.IsEgress {
			t.Fatal("expected egress rule")
		}
		if r.Protocol != ProtocolTCP {
			t.Fatalf("expected TCP, got %d", r.Protocol)
		}
		if r.DstPort != 443 {
			t.Fatalf("expected port 443, got %d", r.DstPort)
		}
		if r.CIDR == "" {
			t.Fatal("expected CIDR to be set for FQDN rule")
		}
	}

	// Verify specific CIDRs.
	cidrs := make(map[string]bool)
	for _, r := range rules {
		cidrs[r.CIDR] = true
	}
	if !cidrs["93.184.216.34/32"] {
		t.Fatal("missing CIDR for 93.184.216.34/32")
	}
	if !cidrs["93.184.216.35/32"] {
		t.Fatal("missing CIDR for 93.184.216.35/32")
	}
}

func TestExtendedCompileIngressAndEgress(t *testing.T) {
	c := testExtendedCompiler()

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "both-directions",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{
				v1alpha1.PolicyTypeIngress,
				v1alpha1.PolicyTypeEgress,
			},
			// No rules = default deny both.
		},
	}

	rules := c.CompilePolicy(nnp)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (deny ingress + deny egress), got %d", len(rules))
	}

	foundIngress := false
	foundEgress := false
	for _, r := range rules {
		if r.Action != ActionDeny {
			t.Fatalf("expected deny action, got %d", r.Action)
		}
		if r.IsEgress {
			foundEgress = true
		} else {
			foundIngress = true
		}
	}
	if !foundIngress {
		t.Fatal("missing ingress deny rule")
	}
	if !foundEgress {
		t.Fatal("missing egress deny rule")
	}
}

func TestExtendedCompileServiceAccountPeer(t *testing.T) {
	c := testExtendedCompiler()

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sa-policy",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeIngress},
			Ingress: []v1alpha1.NovaNetworkPolicyIngressRule{
				{
					From: []v1alpha1.NovaNetworkPolicyPeer{
						{
							ServiceAccount: &v1alpha1.ServiceAccountPeer{
								Name:      "api-sa",
								Namespace: "backend",
							},
						},
					},
				},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule for service account peer, got %d", len(rules))
	}

	r := rules[0]
	if r.Action != ActionAllow {
		t.Fatalf("expected allow action, got %d", r.Action)
	}
	if r.SrcIdentity == WildcardIdentity {
		t.Fatal("expected non-wildcard source identity for service account peer")
	}

	// Verify the identity matches the expected hash.
	expectedID := identity.HashLabels(map[string]string{
		"novanet.io/namespace":       "backend",
		"novanet.io/service-account": "api-sa",
	})
	if r.SrcIdentity != expectedID {
		t.Fatalf("expected identity %d for SA peer, got %d", expectedID, r.SrcIdentity)
	}
}

func TestExtendedCompileServiceAccountDefaultNamespace(t *testing.T) {
	c := testExtendedCompiler()

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sa-default-ns",
			Namespace: "myns",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeIngress},
			Ingress: []v1alpha1.NovaNetworkPolicyIngressRule{
				{
					From: []v1alpha1.NovaNetworkPolicyPeer{
						{
							ServiceAccount: &v1alpha1.ServiceAccountPeer{
								Name: "worker-sa",
								// No namespace -> use policy namespace.
							},
						},
					},
				},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}

	expectedID := identity.HashLabels(map[string]string{
		"novanet.io/namespace":       "myns",
		"novanet.io/service-account": "worker-sa",
	})
	if rules[0].SrcIdentity != expectedID {
		t.Fatalf("expected SA identity from policy namespace, got %d", rules[0].SrcIdentity)
	}
}

func TestExtendedCompileAll(t *testing.T) {
	c := testExtendedCompiler()

	policies := []*v1alpha1.NovaNetworkPolicy{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
			Spec: v1alpha1.NovaNetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "web"},
				},
				PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeIngress},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "default"},
			Spec: v1alpha1.NovaNetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "api"},
				},
				PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeEgress},
			},
		},
	}

	rules := c.CompileAll(policies)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (1 per policy), got %d", len(rules))
	}
}

func TestExtendedCompileDefaultIngress(t *testing.T) {
	c := testExtendedCompiler()

	// No PolicyTypes => default Ingress.
	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-types",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule (default deny ingress), got %d", len(rules))
	}
	if rules[0].Action != ActionDeny {
		t.Fatalf("expected deny, got %d", rules[0].Action)
	}
	if rules[0].IsEgress {
		t.Fatal("expected ingress rule, got egress")
	}
}

func TestExtendedCompileUDPProtocol(t *testing.T) {
	c := testExtendedCompiler()

	proto := "UDP"
	port := int32(53)

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "udp-policy",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeEgress},
			Egress: []v1alpha1.NovaNetworkPolicyEgressRule{
				{
					Ports: []v1alpha1.NovaNetworkPolicyPort{
						{Protocol: &proto, Port: &port},
					},
				},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Protocol != ProtocolUDP {
		t.Fatalf("expected UDP protocol (%d), got %d", ProtocolUDP, rules[0].Protocol)
	}
	if rules[0].DstPort != 53 {
		t.Fatalf("expected port 53, got %d", rules[0].DstPort)
	}
}

func TestExtendedCompileFQDNSingleIP(t *testing.T) {
	c := testExtendedCompiler()
	logger, _ := zap.NewDevelopment()
	c.SetDNSCache(mockDNSCache(logger))

	fqdn := "db.internal"

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fqdn-single",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeEgress},
			Egress: []v1alpha1.NovaNetworkPolicyEgressRule{
				{
					To: []v1alpha1.NovaNetworkPolicyPeer{
						{FQDN: &fqdn},
					},
				},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].CIDR != "10.0.0.50/32" {
		t.Fatalf("expected CIDR 10.0.0.50/32, got %s", rules[0].CIDR)
	}
}

func TestExtendedCompileIngressWithPodSelector(t *testing.T) {
	c := testExtendedCompiler()

	proto := protocolStrTCP
	port := int32(80)

	nnp := &v1alpha1.NovaNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-selector",
			Namespace: "default",
		},
		Spec: v1alpha1.NovaNetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []v1alpha1.PolicyType{v1alpha1.PolicyTypeIngress},
			Ingress: []v1alpha1.NovaNetworkPolicyIngressRule{
				{
					From: []v1alpha1.NovaNetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "api"},
							},
						},
					},
					Ports: []v1alpha1.NovaNetworkPolicyPort{
						{Protocol: &proto, Port: &port},
					},
				},
			},
		},
	}

	rules := c.CompilePolicy(nnp)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}

	r := rules[0]
	if r.Action != ActionAllow {
		t.Fatalf("expected allow, got %d", r.Action)
	}
	if r.Protocol != ProtocolTCP {
		t.Fatalf("expected TCP, got %d", r.Protocol)
	}
	if r.DstPort != 80 {
		t.Fatalf("expected port 80, got %d", r.DstPort)
	}
	if r.SrcIdentity == WildcardIdentity {
		t.Fatal("expected non-wildcard source")
	}
}
