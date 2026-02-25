package k8s

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func TestWatchersNodeEvents(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	w := NewWatchers(clientset, "test-node", testLogger())

	var mu sync.Mutex
	var addedNodes []*corev1.Node
	var deletedNodes []*corev1.Node

	w.OnNodeAdd = func(node *corev1.Node) {
		mu.Lock()
		addedNodes = append(addedNodes, node)
		mu.Unlock()
	}
	w.OnNodeDelete = func(node *corev1.Node) {
		mu.Lock()
		deletedNodes = append(deletedNodes, node)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start watchers: %v", err)
	}
	if err := w.WaitForSync(ctx); err != nil {
		t.Fatalf("failed to sync: %v", err)
	}

	// Create a node.
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-1",
		},
	}
	_, err := clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	// Wait for the event.
	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		count := len(addedNodes)
		mu.Unlock()
		if count > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for node add event")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	mu.Lock()
	if len(addedNodes) != 1 {
		t.Fatalf("expected 1 added node, got %d", len(addedNodes))
	}
	if addedNodes[0].Name != "worker-1" {
		t.Fatalf("expected node worker-1, got %s", addedNodes[0].Name)
	}
	mu.Unlock()

	// Delete the node.
	err = clientset.CoreV1().Nodes().Delete(ctx, "worker-1", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("failed to delete node: %v", err)
	}

	deadline = time.After(5 * time.Second)
	for {
		mu.Lock()
		count := len(deletedNodes)
		mu.Unlock()
		if count > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for node delete event")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestWatchersPodFilterLocalNode(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	w := NewWatchers(clientset, "test-node", testLogger())

	var mu sync.Mutex
	var addedPods []*corev1.Pod

	w.OnPodAdd = func(pod *corev1.Pod) {
		mu.Lock()
		addedPods = append(addedPods, pod)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start watchers: %v", err)
	}
	if err := w.WaitForSync(ctx); err != nil {
		t.Fatalf("failed to sync: %v", err)
	}

	// Create a pod on this node.
	localPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "local-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}
	_, err := clientset.CoreV1().Pods("default").Create(ctx, localPod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create local pod: %v", err)
	}

	// Create a pod on a different node.
	remotePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "remote-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "other-node",
		},
	}
	_, err = clientset.CoreV1().Pods("default").Create(ctx, remotePod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create remote pod: %v", err)
	}

	// Wait for events to settle.
	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		count := len(addedPods)
		mu.Unlock()
		if count > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for pod add event")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Give time for the remote pod event (which should NOT arrive).
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Only the local pod should be reported.
	if len(addedPods) != 1 {
		t.Fatalf("expected 1 added pod (local only), got %d", len(addedPods))
	}
	if addedPods[0].Name != "local-pod" {
		t.Fatalf("expected local-pod, got %s", addedPods[0].Name)
	}
}

func TestWatchersNamespaceEvents(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	w := NewWatchers(clientset, "test-node", testLogger())

	var mu sync.Mutex
	var addedNS []*corev1.Namespace

	w.OnNamespaceAdd = func(ns *corev1.Namespace) {
		mu.Lock()
		addedNS = append(addedNS, ns)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start watchers: %v", err)
	}
	if err := w.WaitForSync(ctx); err != nil {
		t.Fatalf("failed to sync: %v", err)
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}
	_, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		count := len(addedNS)
		mu.Unlock()
		if count > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for namespace add event")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	mu.Lock()
	if len(addedNS) != 1 {
		t.Fatalf("expected 1 added namespace, got %d", len(addedNS))
	}
	if addedNS[0].Name != "test-namespace" {
		t.Fatalf("expected test-namespace, got %s", addedNS[0].Name)
	}
	mu.Unlock()
}

func TestWatchersNetworkPolicyEvents(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	w := NewWatchers(clientset, "test-node", testLogger())

	var mu sync.Mutex
	var addedPolicies []*networkingv1.NetworkPolicy

	w.OnNetworkPolicyAdd = func(np *networkingv1.NetworkPolicy) {
		mu.Lock()
		addedPolicies = append(addedPolicies, np)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start watchers: %v", err)
	}
	if err := w.WaitForSync(ctx); err != nil {
		t.Fatalf("failed to sync: %v", err)
	}

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
		},
	}
	_, err := clientset.NetworkingV1().NetworkPolicies("default").Create(ctx, np, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create network policy: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		count := len(addedPolicies)
		mu.Unlock()
		if count > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for network policy add event")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	mu.Lock()
	if len(addedPolicies) != 1 {
		t.Fatalf("expected 1 added policy, got %d", len(addedPolicies))
	}
	if addedPolicies[0].Name != "test-policy" {
		t.Fatalf("expected test-policy, got %s", addedPolicies[0].Name)
	}
	mu.Unlock()
}

func TestWatchersNoCallbacks(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	w := NewWatchers(clientset, "test-node", testLogger())

	// Do not set any callbacks. Verify no panic occurs.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start watchers: %v", err)
	}
	if err := w.WaitForSync(ctx); err != nil {
		t.Fatalf("failed to sync: %v", err)
	}

	// Create a node — should not panic even without callbacks.
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-1",
		},
	}
	_, err := clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	// Give time for event processing.
	time.Sleep(200 * time.Millisecond)
}

func TestWatchersPodUpdateEvent(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	w := NewWatchers(clientset, "test-node", testLogger())

	var mu sync.Mutex
	var updatedPods []*corev1.Pod

	w.OnPodAdd = func(pod *corev1.Pod) {}
	w.OnPodUpdate = func(old, new *corev1.Pod) {
		mu.Lock()
		updatedPods = append(updatedPods, new)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start watchers: %v", err)
	}
	if err := w.WaitForSync(ctx); err != nil {
		t.Fatalf("failed to sync: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}
	created, err := clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	// Wait for add event.
	time.Sleep(200 * time.Millisecond)

	// Update the pod.
	created.Labels = map[string]string{"updated": "true"}
	_, err = clientset.CoreV1().Pods("default").Update(ctx, created, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to update pod: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		count := len(updatedPods)
		mu.Unlock()
		if count > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for pod update event")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	mu.Lock()
	if len(updatedPods) == 0 {
		t.Fatal("expected at least 1 updated pod event")
	}
	mu.Unlock()
}
