package lbipam

import (
	"net"
	"testing"

	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	l, _ := zap.NewDevelopment()
	return l
}

func TestAllocateFromPool(t *testing.T) {
	a := NewAllocator(testLogger())
	if err := a.AddPool("test", "10.0.0.0/30"); err != nil {
		t.Fatalf("AddPool: %v", err)
	}

	ip, err := a.Allocate("default/svc1")
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if ip == nil {
		t.Fatal("expected non-nil IP")
	}

	svc, ok := a.GetServiceForIP(ip)
	if !ok || svc != "default/svc1" {
		t.Fatalf("GetServiceForIP: got %q, %v", svc, ok)
	}
}

func TestAllocateSpecificPool(t *testing.T) {
	a := NewAllocator(testLogger())
	if err := a.AddPool("pool-a", "10.0.0.0/30"); err != nil {
		t.Fatalf("AddPool pool-a: %v", err)
	}
	if err := a.AddPool("pool-b", "10.0.1.0/30"); err != nil {
		t.Fatalf("AddPool pool-b: %v", err)
	}

	ip, err := a.AllocateFromPool("pool-b", "default/svc1")
	if err != nil {
		t.Fatalf("AllocateFromPool: %v", err)
	}

	_, ipNet, _ := net.ParseCIDR("10.0.1.0/30")
	if !ipNet.Contains(ip) {
		t.Fatalf("expected IP in pool-b range, got %s", ip)
	}
}

func TestAllocateFromPoolNotFound(t *testing.T) {
	a := NewAllocator(testLogger())
	_, err := a.AllocateFromPool("missing", "default/svc1")
	if err == nil {
		t.Fatal("expected error for missing pool")
	}
}

func TestReleaseAndReAllocate(t *testing.T) {
	a := NewAllocator(testLogger())
	if err := a.AddPool("test", "10.0.0.0/30"); err != nil {
		t.Fatalf("AddPool: %v", err)
	}

	ip1, err := a.Allocate("default/svc1")
	if err != nil {
		t.Fatalf("first Allocate: %v", err)
	}

	if !a.Release(ip1) {
		t.Fatal("Release returned false")
	}

	// After release, the same IP should be re-allocatable.
	ip2, err := a.Allocate("default/svc2")
	if err != nil {
		t.Fatalf("second Allocate: %v", err)
	}
	if !ip1.Equal(ip2) {
		t.Fatalf("expected re-allocated IP %s, got %s", ip1, ip2)
	}
}

func TestReleaseUnknownIP(t *testing.T) {
	a := NewAllocator(testLogger())
	if a.Release(net.ParseIP("192.168.1.1")) {
		t.Fatal("expected Release to return false for unknown IP")
	}
}

func TestPoolExhaustion(t *testing.T) {
	a := NewAllocator(testLogger())
	// /30 = 4 addresses
	if err := a.AddPool("tiny", "10.0.0.0/30"); err != nil {
		t.Fatalf("AddPool: %v", err)
	}

	for i := 0; i < 4; i++ {
		_, err := a.Allocate("default/svc" + string(rune('0'+i)))
		if err != nil {
			t.Fatalf("Allocate %d: %v", i, err)
		}
	}

	// 5th allocation should fail.
	_, err := a.Allocate("default/overflow")
	if err == nil {
		t.Fatal("expected error on pool exhaustion")
	}
}

func TestMultiplePools(t *testing.T) {
	a := NewAllocator(testLogger())
	// /31 = 2 addresses each
	if err := a.AddPool("p1", "10.0.0.0/31"); err != nil {
		t.Fatalf("AddPool p1: %v", err)
	}
	if err := a.AddPool("p2", "10.0.1.0/31"); err != nil {
		t.Fatalf("AddPool p2: %v", err)
	}

	// Allocate 2 from p1 (fills it), then next goes to p2.
	for i := 0; i < 2; i++ {
		_, err := a.Allocate("default/svc" + string(rune('a'+i)))
		if err != nil {
			t.Fatalf("Allocate %d: %v", i, err)
		}
	}

	ip3, err := a.Allocate("default/svc-c")
	if err != nil {
		t.Fatalf("Allocate from second pool: %v", err)
	}

	_, p2Net, _ := net.ParseCIDR("10.0.1.0/31")
	if !p2Net.Contains(ip3) {
		t.Fatalf("expected IP in p2 range, got %s", ip3)
	}
}

func TestDuplicatePoolName(t *testing.T) {
	a := NewAllocator(testLogger())
	if err := a.AddPool("dup", "10.0.0.0/24"); err != nil {
		t.Fatalf("first AddPool: %v", err)
	}
	if err := a.AddPool("dup", "10.0.1.0/24"); err == nil {
		t.Fatal("expected error for duplicate pool name")
	}
}

func TestRemovePool(t *testing.T) {
	a := NewAllocator(testLogger())
	if err := a.AddPool("rm", "10.0.0.0/24"); err != nil {
		t.Fatalf("AddPool: %v", err)
	}
	a.RemovePool("rm")

	_, err := a.AllocateFromPool("rm", "default/svc1")
	if err == nil {
		t.Fatal("expected error after pool removal")
	}
}

func TestListAllocations(t *testing.T) {
	a := NewAllocator(testLogger())
	if err := a.AddPool("test", "10.0.0.0/24"); err != nil {
		t.Fatalf("AddPool: %v", err)
	}

	ip1, _ := a.Allocate("default/svc1")
	ip2, _ := a.Allocate("default/svc2")

	allocs := a.ListAllocations()
	if len(allocs) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(allocs))
	}
	if allocs[ip1.String()] != "default/svc1" {
		t.Fatalf("wrong service for %s", ip1)
	}
	if allocs[ip2.String()] != "default/svc2" {
		t.Fatalf("wrong service for %s", ip2)
	}
}

func TestInvalidCIDR(t *testing.T) {
	a := NewAllocator(testLogger())
	err := a.AddPool("bad", "not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}
