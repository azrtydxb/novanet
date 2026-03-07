package l2announce

import (
	"net"
	"testing"

	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	l, _ := zap.NewDevelopment()
	return l
}

func TestAddAndListIPs(t *testing.T) {
	a := NewAnnouncer("lo", testLogger())

	// AddIP may return an error on non-Linux platforms; we ignore it
	// here because we're testing the tracking logic.
	_ = a.AddIP(net.ParseIP("10.0.0.1"))
	_ = a.AddIP(net.ParseIP("10.0.0.2"))

	ips := a.ListIPs()
	if len(ips) != 2 {
		t.Fatalf("expected 2 IPs, got %d", len(ips))
	}
	// ListIPs returns sorted order.
	if ips[0].String() != "10.0.0.1" {
		t.Fatalf("expected 10.0.0.1, got %s", ips[0])
	}
	if ips[1].String() != "10.0.0.2" {
		t.Fatalf("expected 10.0.0.2, got %s", ips[1])
	}
}

func TestAddDuplicateIP(t *testing.T) {
	a := NewAnnouncer("lo", testLogger())

	_ = a.AddIP(net.ParseIP("10.0.0.1"))
	_ = a.AddIP(net.ParseIP("10.0.0.1"))

	ips := a.ListIPs()
	if len(ips) != 1 {
		t.Fatalf("expected 1 IP after duplicate add, got %d", len(ips))
	}
}

func TestRemoveIP(t *testing.T) {
	a := NewAnnouncer("lo", testLogger())

	_ = a.AddIP(net.ParseIP("10.0.0.1"))
	_ = a.AddIP(net.ParseIP("10.0.0.2"))

	a.RemoveIP(net.ParseIP("10.0.0.1"))

	ips := a.ListIPs()
	if len(ips) != 1 {
		t.Fatalf("expected 1 IP after removal, got %d", len(ips))
	}
	if ips[0].String() != "10.0.0.2" {
		t.Fatalf("expected 10.0.0.2, got %s", ips[0])
	}
}

func TestRemoveNonexistentIP(t *testing.T) {
	a := NewAnnouncer("lo", testLogger())
	// Should not panic.
	a.RemoveIP(net.ParseIP("10.0.0.99"))
}

func TestListIPsEmpty(t *testing.T) {
	a := NewAnnouncer("lo", testLogger())
	ips := a.ListIPs()
	if len(ips) != 0 {
		t.Fatalf("expected 0 IPs, got %d", len(ips))
	}
}

func TestAnnounceAllNonLinux(t *testing.T) {
	a := NewAnnouncer("lo", testLogger())

	_ = a.AddIP(net.ParseIP("10.0.0.1"))

	// On non-Linux, AnnounceAll returns an error.
	// On Linux, it would attempt to send packets.
	// We just verify it doesn't panic.
	_ = a.AnnounceAll()
}
