package node

import (
	"sync"
	"testing"

	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func TestAddNode(t *testing.T) {
	r := NewRegistry(testLogger())

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")

	node, ok := r.GetNode("node-1")
	if !ok {
		t.Fatal("expected to find node-1")
	}
	if node.Name != "node-1" {
		t.Fatalf("expected name node-1, got %s", node.Name)
	}
	if node.IP != "10.0.0.1" {
		t.Fatalf("expected IP 10.0.0.1, got %s", node.IP)
	}
	if node.PodCIDR != "10.244.1.0/24" {
		t.Fatalf("expected PodCIDR 10.244.1.0/24, got %s", node.PodCIDR)
	}
	if !node.Ready {
		t.Fatal("expected node to be ready")
	}
}

func TestAddNodeUpdate(t *testing.T) {
	r := NewRegistry(testLogger())

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")
	r.AddNode("node-1", "10.0.0.2", "10.244.1.0/24")

	node, ok := r.GetNode("node-1")
	if !ok {
		t.Fatal("expected to find node-1")
	}
	if node.IP != "10.0.0.2" {
		t.Fatalf("expected updated IP 10.0.0.2, got %s", node.IP)
	}
}

func TestRemoveNode(t *testing.T) {
	r := NewRegistry(testLogger())

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")
	r.RemoveNode("node-1")

	_, ok := r.GetNode("node-1")
	if ok {
		t.Fatal("expected node-1 to be removed")
	}
}

func TestRemoveNonExistentNode(t *testing.T) {
	r := NewRegistry(testLogger())

	// Should not panic.
	r.RemoveNode("nonexistent")
}

func TestGetNodeNotFound(t *testing.T) {
	r := NewRegistry(testLogger())

	_, ok := r.GetNode("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestGetNodeReturnsCopy(t *testing.T) {
	r := NewRegistry(testLogger())

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")

	node, _ := r.GetNode("node-1")
	node.IP = "modified"

	original, _ := r.GetNode("node-1")
	if original.IP != "10.0.0.1" {
		t.Fatal("modifying returned node affected stored node")
	}
}

func TestListNodes(t *testing.T) {
	r := NewRegistry(testLogger())

	r.AddNode("node-c", "10.0.0.3", "10.244.3.0/24")
	r.AddNode("node-a", "10.0.0.1", "10.244.1.0/24")
	r.AddNode("node-b", "10.0.0.2", "10.244.2.0/24")

	nodes := r.ListNodes()
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	// Should be sorted by name.
	if nodes[0].Name != "node-a" {
		t.Fatalf("expected first node node-a, got %s", nodes[0].Name)
	}
	if nodes[1].Name != "node-b" {
		t.Fatalf("expected second node node-b, got %s", nodes[1].Name)
	}
	if nodes[2].Name != "node-c" {
		t.Fatalf("expected third node node-c, got %s", nodes[2].Name)
	}
}

func TestListNodesEmpty(t *testing.T) {
	r := NewRegistry(testLogger())

	nodes := r.ListNodes()
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestOnNodeChangeAdd(t *testing.T) {
	r := NewRegistry(testLogger())

	var gotEvent string
	var gotNode *NodeInfo
	r.OnNodeChange(func(event string, node *NodeInfo) {
		gotEvent = event
		gotNode = node
	})

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")

	if gotEvent != "add" {
		t.Fatalf("expected event add, got %s", gotEvent)
	}
	if gotNode == nil || gotNode.Name != "node-1" {
		t.Fatal("expected node-1 in callback")
	}
}

func TestOnNodeChangeUpdate(t *testing.T) {
	r := NewRegistry(testLogger())

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")

	var gotEvent string
	r.OnNodeChange(func(event string, node *NodeInfo) {
		gotEvent = event
	})

	r.AddNode("node-1", "10.0.0.2", "10.244.1.0/24")

	if gotEvent != "update" {
		t.Fatalf("expected event update, got %s", gotEvent)
	}
}

func TestOnNodeChangeDelete(t *testing.T) {
	r := NewRegistry(testLogger())

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")

	var gotEvent string
	var gotNode *NodeInfo
	r.OnNodeChange(func(event string, node *NodeInfo) {
		gotEvent = event
		gotNode = node
	})

	r.RemoveNode("node-1")

	if gotEvent != "delete" {
		t.Fatalf("expected event delete, got %s", gotEvent)
	}
	if gotNode == nil || gotNode.Name != "node-1" {
		t.Fatal("expected node-1 in delete callback")
	}
}

func TestSetReady(t *testing.T) {
	r := NewRegistry(testLogger())

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")

	r.SetReady("node-1", false)

	node, ok := r.GetNode("node-1")
	if !ok {
		t.Fatal("expected to find node-1")
	}
	if node.Ready {
		t.Fatal("expected node to not be ready")
	}

	r.SetReady("node-1", true)

	node, _ = r.GetNode("node-1")
	if !node.Ready {
		t.Fatal("expected node to be ready")
	}
}

func TestSetReadyNonExistent(t *testing.T) {
	r := NewRegistry(testLogger())

	// Should not panic.
	r.SetReady("nonexistent", true)
}

func TestCount(t *testing.T) {
	r := NewRegistry(testLogger())

	if r.Count() != 0 {
		t.Fatalf("expected 0 nodes, got %d", r.Count())
	}

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")
	r.AddNode("node-2", "10.0.0.2", "10.244.2.0/24")

	if r.Count() != 2 {
		t.Fatalf("expected 2 nodes, got %d", r.Count())
	}

	r.RemoveNode("node-1")

	if r.Count() != 1 {
		t.Fatalf("expected 1 node, got %d", r.Count())
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewRegistry(testLogger())

	var wg sync.WaitGroup

	// Concurrent adds.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "node-" + string(rune('a'+i%26))
			r.AddNode(name, "10.0.0.1", "10.244.0.0/24")
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.ListNodes()
		}()
	}

	wg.Wait()
}

func TestMultipleCallbacks(t *testing.T) {
	r := NewRegistry(testLogger())

	callCount := 0
	r.OnNodeChange(func(event string, node *NodeInfo) {
		callCount++
	})
	r.OnNodeChange(func(event string, node *NodeInfo) {
		callCount++
	})

	r.AddNode("node-1", "10.0.0.1", "10.244.1.0/24")

	if callCount != 2 {
		t.Fatalf("expected 2 callbacks, got %d", callCount)
	}
}
