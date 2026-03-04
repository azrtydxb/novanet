package egress

import (
	"net"
	"sync"
	"testing"

	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func testManager() *Manager {
	_, clusterCIDR, _ := net.ParseCIDR("10.244.0.0/16")
	return NewManager(net.ParseIP("10.0.0.1"), clusterCIDR, testLogger())
}

func TestNewManager(t *testing.T) {
	m := testManager()
	if m == nil {
		t.Fatal("manager is nil")
	}
	if m.NodeIP().String() != "10.0.0.1" {
		t.Fatalf("expected node IP 10.0.0.1, got %s", m.NodeIP())
	}
	if m.ClusterCIDR().String() != "10.244.0.0/16" {
		t.Fatalf("expected cluster CIDR 10.244.0.0/16, got %s", m.ClusterCIDR())
	}
}

func TestSetMasqueradeEnabled(t *testing.T) {
	m := testManager()

	if !m.IsMasqueradeEnabled() {
		t.Fatal("expected masquerade to be enabled by default")
	}

	m.SetMasqueradeEnabled(false)
	if m.IsMasqueradeEnabled() {
		t.Fatal("expected masquerade to be disabled")
	}

	m.SetMasqueradeEnabled(true)
	if !m.IsMasqueradeEnabled() {
		t.Fatal("expected masquerade to be enabled")
	}
}

func TestAddEgressRule(t *testing.T) {
	m := testManager()

	err := m.AddEgressRule("default", Rule{
		Name:        "allow-external",
		SrcIdentity: 100,
		DstCIDR:     "0.0.0.0/0",
		Protocol:    6,
		DstPort:     443,
		Action:      ActionAllow,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Count() != 1 {
		t.Fatalf("expected 1 rule, got %d", m.Count())
	}

	rule, ok := m.GetRule("default", "allow-external")
	if !ok {
		t.Fatal("expected to find rule")
	}
	if rule.SrcIdentity != 100 {
		t.Fatalf("expected src identity 100, got %d", rule.SrcIdentity)
	}
	if rule.DstCIDR.String() != "0.0.0.0/0" {
		t.Fatalf("expected dst CIDR 0.0.0.0/0, got %s", rule.DstCIDR.String())
	}
	if rule.Protocol != 6 {
		t.Fatalf("expected protocol 6, got %d", rule.Protocol)
	}
	if rule.DstPort != 443 {
		t.Fatalf("expected dst port 443, got %d", rule.DstPort)
	}
	if rule.Action != ActionAllow {
		t.Fatalf("expected action allow, got %d", rule.Action)
	}
}

func TestAddEgressRuleInvalidCIDR(t *testing.T) {
	m := testManager()

	err := m.AddEgressRule("default", Rule{
		Name:    "bad-rule",
		DstCIDR: "not-a-cidr",
	})
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestAddEgressRuleOverwrite(t *testing.T) {
	m := testManager()

	err := m.AddEgressRule("default", Rule{
		Name:    "rule-1",
		DstCIDR: "0.0.0.0/0",
		Action:  ActionAllow,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Overwrite with different action.
	err = m.AddEgressRule("default", Rule{
		Name:    "rule-1",
		DstCIDR: "0.0.0.0/0",
		Action:  ActionDeny,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Count() != 1 {
		t.Fatalf("expected 1 rule after overwrite, got %d", m.Count())
	}

	rule, _ := m.GetRule("default", "rule-1")
	if rule.Action != ActionDeny {
		t.Fatalf("expected action deny after overwrite, got %d", rule.Action)
	}
}

func TestRemoveEgressRule(t *testing.T) {
	m := testManager()

	_ = m.AddEgressRule("default", Rule{
		Name:    "rule-1",
		DstCIDR: "0.0.0.0/0",
	})

	m.RemoveEgressRule("default", "rule-1")

	if m.Count() != 0 {
		t.Fatalf("expected 0 rules after remove, got %d", m.Count())
	}

	_, ok := m.GetRule("default", "rule-1")
	if ok {
		t.Fatal("expected rule to be removed")
	}
}

func TestRemoveNonExistentRule(t *testing.T) {
	m := testManager()

	// Should not panic.
	m.RemoveEgressRule("default", "nonexistent")
}

func TestGetRules(t *testing.T) {
	m := testManager()

	_ = m.AddEgressRule("default", Rule{
		Name:    "rule-1",
		DstCIDR: "0.0.0.0/0",
		Action:  ActionAllow,
	})
	_ = m.AddEgressRule("kube-system", Rule{
		Name:    "rule-2",
		DstCIDR: "10.0.0.0/8",
		Action:  ActionSNAT,
	})

	rules := m.GetRules()
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
}

func TestGetRulesEmpty(t *testing.T) {
	m := testManager()

	rules := m.GetRules()
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
}

func TestSameNameDifferentNamespace(t *testing.T) {
	m := testManager()

	_ = m.AddEgressRule("ns-a", Rule{
		Name:    "rule-1",
		DstCIDR: "0.0.0.0/0",
		Action:  ActionAllow,
	})
	_ = m.AddEgressRule("ns-b", Rule{
		Name:    "rule-1",
		DstCIDR: "10.0.0.0/8",
		Action:  ActionDeny,
	})

	if m.Count() != 2 {
		t.Fatalf("expected 2 rules from different namespaces, got %d", m.Count())
	}

	ruleA, ok := m.GetRule("ns-a", "rule-1")
	if !ok {
		t.Fatal("expected to find ns-a/rule-1")
	}
	if ruleA.Action != ActionAllow {
		t.Fatalf("expected action allow for ns-a, got %d", ruleA.Action)
	}

	ruleB, ok := m.GetRule("ns-b", "rule-1")
	if !ok {
		t.Fatal("expected to find ns-b/rule-1")
	}
	if ruleB.Action != ActionDeny {
		t.Fatalf("expected action deny for ns-b, got %d", ruleB.Action)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := testManager()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "rule-" + string(rune('a'+i%26))
			_ = m.AddEgressRule("default", Rule{
				Name:    name,
				DstCIDR: "0.0.0.0/0",
				Action:  ActionAllow,
			})
		}(i)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.GetRules()
		}()
	}

	wg.Wait()
}

func TestSNATAction(t *testing.T) {
	m := testManager()

	err := m.AddEgressRule("default", Rule{
		Name:        "snat-rule",
		SrcIdentity: 200,
		DstCIDR:     "8.8.8.0/24",
		Action:      ActionSNAT,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rule, ok := m.GetRule("default", "snat-rule")
	if !ok {
		t.Fatal("expected to find snat-rule")
	}
	if rule.Action != ActionSNAT {
		t.Fatalf("expected SNAT action, got %d", rule.Action)
	}
}
