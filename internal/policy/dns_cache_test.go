package policy

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func testDNSCache() *DNSCache {
	logger, _ := zap.NewDevelopment()
	return NewDNSCache(logger, defaultMaxEntries)
}

func TestDNSCacheResolve(t *testing.T) {
	cache := testDNSCache()

	callCount := atomic.Int32{}
	cache.SetResolver(func(_ context.Context, host string) ([]net.IP, error) {
		callCount.Add(1)
		if host == "example.com" {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		return nil, &net.DNSError{Err: "not found", Name: host}
	})

	ips := cache.Resolve("example.com")
	if len(ips) != 1 {
		t.Fatalf("expected 1 IP, got %d", len(ips))
	}
	if ips[0].String() != "93.184.216.34" {
		t.Fatalf("expected 93.184.216.34, got %s", ips[0].String())
	}
	if callCount.Load() != 1 {
		t.Fatalf("expected 1 resolver call, got %d", callCount.Load())
	}

	ips = cache.Resolve("example.com")
	if len(ips) != 1 {
		t.Fatalf("expected 1 IP from cache, got %d", len(ips))
	}
	if callCount.Load() != 1 {
		t.Fatalf("expected still 1 resolver call (cached), got %d", callCount.Load())
	}
}

func TestDNSCacheResolveNotFound(t *testing.T) {
	cache := testDNSCache()

	cache.SetResolver(func(_ context.Context, host string) ([]net.IP, error) {
		return nil, &net.DNSError{Err: "not found", Name: host}
	})

	ips := cache.Resolve("nonexistent.example.com")
	if ips != nil {
		t.Fatalf("expected nil for failed resolution, got %v", ips)
	}
}

func TestDNSCacheRefresh(t *testing.T) {
	cache := testDNSCache()

	callCount := atomic.Int32{}
	cache.SetResolver(func(_ context.Context, host string) ([]net.IP, error) {
		c := callCount.Add(1)
		if host == "example.com" {
			if c <= 1 {
				return []net.IP{net.ParseIP("1.1.1.1")}, nil
			}
			return []net.IP{net.ParseIP("2.2.2.2")}, nil
		}
		return nil, &net.DNSError{Err: "not found", Name: host}
	})

	ips := cache.Resolve("example.com")
	if len(ips) != 1 || ips[0].String() != "1.1.1.1" {
		t.Fatalf("expected 1.1.1.1, got %v", ips)
	}

	changed := cache.Refresh()
	if changed != 1 {
		t.Fatalf("expected 1 changed entry, got %d", changed)
	}

	all := cache.GetAll()
	if len(all["example.com"]) != 1 || all["example.com"][0].String() != "2.2.2.2" {
		t.Fatalf("expected 2.2.2.2 after refresh, got %v", all["example.com"])
	}
}

func TestDNSCacheRefreshNoChange(t *testing.T) {
	cache := testDNSCache()

	cache.SetResolver(func(_ context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.1.1.1")}, nil
	})

	cache.Resolve("stable.example.com")

	changed := cache.Refresh()
	if changed != 0 {
		t.Fatalf("expected 0 changed entries, got %d", changed)
	}
}

func TestDNSCacheGetAll(t *testing.T) {
	cache := testDNSCache()

	cache.SetResolver(func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "a.example.com":
			return []net.IP{net.ParseIP("1.1.1.1")}, nil
		case "b.example.com":
			return []net.IP{net.ParseIP("2.2.2.2"), net.ParseIP("3.3.3.3")}, nil
		}
		return nil, &net.DNSError{Err: "not found", Name: host}
	})

	cache.Resolve("a.example.com")
	cache.Resolve("b.example.com")

	all := cache.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	if len(all["a.example.com"]) != 1 {
		t.Fatalf("expected 1 IP for a.example.com, got %d", len(all["a.example.com"]))
	}
	if len(all["b.example.com"]) != 2 {
		t.Fatalf("expected 2 IPs for b.example.com, got %d", len(all["b.example.com"]))
	}
}

func TestIpsEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []net.IP
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []net.IP{}, []net.IP{}, true},
		{"same single", []net.IP{net.ParseIP("1.1.1.1")}, []net.IP{net.ParseIP("1.1.1.1")}, true},
		{"different length", []net.IP{net.ParseIP("1.1.1.1")}, []net.IP{}, false},
		{"different IPs", []net.IP{net.ParseIP("1.1.1.1")}, []net.IP{net.ParseIP("2.2.2.2")}, false},
		{"same unordered", []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2")}, []net.IP{net.ParseIP("2.2.2.2"), net.ParseIP("1.1.1.1")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ipsEqual(tt.a, tt.b); got != tt.want {
				t.Fatalf("ipsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDNSCacheExpiredEntry(t *testing.T) {
	cache := testDNSCache()

	callCount := atomic.Int32{}
	cache.SetResolver(func(_ context.Context, _ string) ([]net.IP, error) {
		c := callCount.Add(1)
		if c == 1 {
			return []net.IP{net.ParseIP("1.1.1.1")}, nil
		}
		return []net.IP{net.ParseIP("2.2.2.2")}, nil
	})

	cache.Resolve("example.com")

	cache.mu.Lock()
	cache.entries["example.com"].expiry = time.Now().Add(-1 * time.Second)
	cache.mu.Unlock()

	ips := cache.Resolve("example.com")
	if len(ips) != 1 || ips[0].String() != "2.2.2.2" {
		t.Fatalf("expected 2.2.2.2 after expiry, got %v", ips)
	}
	if callCount.Load() != 2 {
		t.Fatalf("expected 2 resolver calls after expiry, got %d", callCount.Load())
	}
}

func TestDNSCacheSize(t *testing.T) {
	cache := testDNSCache()

	cache.SetResolver(func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.1.1.1")}, nil
	})

	if cache.Size() != 0 {
		t.Fatalf("expected size 0, got %d", cache.Size())
	}

	cache.Resolve("a.example.com")
	if cache.Size() != 1 {
		t.Fatalf("expected size 1, got %d", cache.Size())
	}

	cache.Resolve("b.example.com")
	if cache.Size() != 2 {
		t.Fatalf("expected size 2, got %d", cache.Size())
	}

	cache.Resolve("a.example.com")
	if cache.Size() != 2 {
		t.Fatalf("expected size 2 after re-resolve, got %d", cache.Size())
	}
}

func TestDNSCacheLRUEviction(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewDNSCache(logger, 3)

	cache.SetResolver(func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.1.1.1")}, nil
	})

	cache.Resolve("a.example.com")
	cache.Resolve("b.example.com")
	cache.Resolve("c.example.com")

	if cache.Size() != 3 {
		t.Fatalf("expected size 3, got %d", cache.Size())
	}

	cache.Resolve("a.example.com")

	cache.Resolve("d.example.com")

	if cache.Size() != 3 {
		t.Fatalf("expected size 3 after eviction, got %d", cache.Size())
	}

	all := cache.GetAll()

	if _, ok := all["d.example.com"]; !ok {
		t.Fatal("expected d.example.com to be present after insertion")
	}

	if _, ok := all["a.example.com"]; !ok {
		t.Fatal("expected a.example.com to be present (recently accessed)")
	}

	if _, ok := all["b.example.com"]; ok {
		t.Fatal("expected b.example.com to be evicted (it was LRU)")
	}
}

func TestDNSCacheLRUEvictionOrder(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewDNSCache(logger, 2)

	cache.SetResolver(func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.1")}, nil
	})

	cache.Resolve("first.example.com")
	cache.Resolve("second.example.com")

	cache.Resolve("first.example.com")

	cache.Resolve("third.example.com")

	if cache.Size() != 2 {
		t.Fatalf("expected size 2, got %d", cache.Size())
	}

	all := cache.GetAll()
	if _, ok := all["first.example.com"]; !ok {
		t.Fatal("expected first.example.com to survive (recently accessed)")
	}
	if _, ok := all["third.example.com"]; !ok {
		t.Fatal("expected third.example.com to be present (just added)")
	}
	if _, ok := all["second.example.com"]; ok {
		t.Fatal("expected second.example.com to be evicted")
	}
}

func TestDNSCacheMaxEntriesDefault(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cache := NewDNSCache(logger, 0)
	if cache.maxEntries != defaultMaxEntries {
		t.Fatalf("expected default max entries %d, got %d", defaultMaxEntries, cache.maxEntries)
	}

	cache = NewDNSCache(logger, -1)
	if cache.maxEntries != defaultMaxEntries {
		t.Fatalf("expected default max entries %d, got %d", defaultMaxEntries, cache.maxEntries)
	}
}

func TestDNSCacheEvictionUnderLoad(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	maxSize := 100
	cache := NewDNSCache(logger, maxSize)

	cache.SetResolver(func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.1")}, nil
	})

	for i := 0; i < maxSize+50; i++ {
		cache.Resolve(fmt.Sprintf("host-%d.example.com", i))
	}

	if cache.Size() != maxSize {
		t.Fatalf("expected cache size %d, got %d", maxSize, cache.Size())
	}

	all := cache.GetAll()
	lastKey := fmt.Sprintf("host-%d.example.com", maxSize+49)
	if _, ok := all[lastKey]; !ok {
		t.Fatalf("expected most recent entry %s to be present", lastKey)
	}
}
