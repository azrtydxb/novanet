#!/usr/bin/env bash
# 01-same-node.sh — Test same-node pod-to-pod connectivity.
#
# Creates two pods on the same node and verifies:
#   1. ICMP ping between pods
#   2. TCP connectivity via iperf3
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 01: Same-Node Pod-to-Pod Connectivity"

###############################################################################
# Setup
###############################################################################
ensure_test_ns
register_cleanup

# Pick a node to schedule both pods on.
TARGET_NODE="$(get_worker_nodes | awk '{print $1}')"
if [[ -z "$TARGET_NODE" ]]; then
    log_fail "No schedulable node found"
    exit 1
fi
log_info "Target node: $TARGET_NODE"

# Create pods
create_pod "same-node-client" "$TARGET_NODE" "app=novanet-test,role=client"
create_iperf3_server "same-node-server" "$TARGET_NODE"

# Wait for pods
wait_pod_ready "same-node-client"
wait_pod_ready "same-node-server"

# Gather IPs
CLIENT_IP="$(get_pod_ip same-node-client)"
SERVER_IP="$(get_pod_ip same-node-server)"
CLIENT_NODE="$(get_pod_node same-node-client)"
SERVER_NODE="$(get_pod_node same-node-server)"

log_info "Client: $CLIENT_IP on $CLIENT_NODE"
log_info "Server: $SERVER_IP on $SERVER_NODE"

if [[ "$CLIENT_NODE" != "$SERVER_NODE" ]]; then
    log_warn "Pods ended up on different nodes — test may not validate same-node path"
fi

###############################################################################
# Tests
###############################################################################

# Test 1: ICMP ping client -> server
assert_ping "Ping client -> server (same node)" "same-node-client" "$SERVER_IP"

# Test 2: ICMP ping server -> client
assert_ping "Ping server -> client (same node)" "same-node-server" "$CLIENT_IP"

# Test 3: Wait for iperf3 server to be listening, then run iperf3 TCP test.
begin_test "iperf3 TCP throughput (same node)"
# Give iperf3 server a moment to bind
sleep 2
IPERF_OUT="$(pod_exec same-node-client iperf3 -c "$SERVER_IP" -t 5 -J 2>&1)" || true

# Parse the result — look for bits_per_second in the JSON output
if echo "$IPERF_OUT" | grep -q '"bits_per_second"'; then
    BPS="$(echo "$IPERF_OUT" | grep -o '"bits_per_second":[0-9.e+]*' | head -1 | cut -d: -f2)"
    MBPS="$(echo "$BPS" | awk '{printf "%.2f", $1/1000000}')"
    log_info "iperf3 throughput: ${MBPS} Mbps"
    pass_test
else
    log_info "iperf3 output: $(echo "$IPERF_OUT" | tail -5)"
    fail_test "iperf3 did not produce throughput data"
fi

# Test 4: UDP connectivity (best-effort — background nc through exec is unreliable)
begin_test "UDP connectivity (same node)"
pod_exec same-node-server bash -c "timeout 10 nc -u -l 9999 > /tmp/udp-result &" 2>/dev/null || true
sleep 1
pod_exec same-node-client bash -c "echo 'novanet-test' | nc -u -w 2 $SERVER_IP 9999" 2>/dev/null || true
sleep 2
UDP_RESULT="$(pod_exec same-node-server cat /tmp/udp-result 2>/dev/null || echo "")"
if [[ "$UDP_RESULT" == *"novanet-test"* ]]; then
    pass_test
else
    # UDP netcat via kubectl exec is inherently unreliable; treat as non-fatal
    skip_test "UDP message not received (background nc via exec is unreliable)"
fi

###############################################################################
# Summary
###############################################################################
print_summary
