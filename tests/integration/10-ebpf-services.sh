#!/usr/bin/env bash
# 10-ebpf-services.sh — Smoke test for the eBPF Services gRPC API.
#
# Verifies that:
#   1. The ebpf-services.sock exists inside each NovaNet agent pod
#   2. The GetSockmapStats RPC responds successfully via novanetctl
#   3. The ListMeshRedirects RPC responds successfully via novanetctl
#   4. The GetBackendHealth RPC responds successfully via novanetctl
#
# Note: This is a smoke test. Full eBPF testing requires a real kernel
# with BPF support and cannot run in most CI environments.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 10: eBPF Services API Smoke Test"

###############################################################################
# Prerequisites
###############################################################################

# Find a NovaNet agent pod.
AGENT_POD="$(kubectl get pods -n "$NOVANET_NS" -l app.kubernetes.io/name=novanet \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")"

if [[ -z "$AGENT_POD" ]]; then
    log_fail "No NovaNet agent pod found in namespace $NOVANET_NS"
    exit 1
fi
log_info "Using agent pod: $AGENT_POD"

EBPF_SOCKET="/run/novanet/ebpf-services.sock"

###############################################################################
# Tests
###############################################################################

# Test 1: Verify the ebpf-services.sock exists in the agent pod.
begin_test "eBPF Services socket exists"
if kubectl exec -n "$NOVANET_NS" "$AGENT_POD" -c agent -- \
    test -S "$EBPF_SOCKET" 2>/dev/null; then
    pass_test
else
    fail_test "Socket $EBPF_SOCKET not found in pod $AGENT_POD"
fi

# Test 2: Call GetSockmapStats via novanetctl.
begin_test "GetSockmapStats RPC responds"
SOCKMAP_OUT="$(kubectl exec -n "$NOVANET_NS" "$AGENT_POD" -c agent -- \
    novanetctl ebpf sockmap status --ebpf-socket "$EBPF_SOCKET" 2>&1)" || true
if echo "$SOCKMAP_OUT" | grep -q "Redirected:"; then
    log_info "SOCKMAP stats retrieved successfully"
    pass_test
else
    fail_test "Unexpected output: $(echo "$SOCKMAP_OUT" | head -5)"
fi

# Test 3: Call ListMeshRedirects via novanetctl.
begin_test "ListMeshRedirects RPC responds"
MESH_OUT="$(kubectl exec -n "$NOVANET_NS" "$AGENT_POD" -c agent -- \
    novanetctl ebpf mesh list --ebpf-socket "$EBPF_SOCKET" 2>&1)" || true
if echo "$MESH_OUT" | grep -q "MESH REDIRECTS"; then
    log_info "Mesh redirects listed successfully"
    pass_test
else
    fail_test "Unexpected output: $(echo "$MESH_OUT" | head -5)"
fi

# Test 4: Call GetBackendHealth via novanetctl.
begin_test "GetBackendHealth RPC responds"
HEALTH_OUT="$(kubectl exec -n "$NOVANET_NS" "$AGENT_POD" -c agent -- \
    novanetctl ebpf health list --ebpf-socket "$EBPF_SOCKET" 2>&1)" || true
if echo "$HEALTH_OUT" | grep -q "BACKEND HEALTH"; then
    log_info "Backend health listed successfully"
    pass_test
else
    fail_test "Unexpected output: $(echo "$HEALTH_OUT" | head -5)"
fi

###############################################################################
# Summary
###############################################################################
print_summary
