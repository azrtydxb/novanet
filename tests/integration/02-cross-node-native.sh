#!/usr/bin/env bash
# 02-cross-node-native.sh — Test cross-node pod-to-pod connectivity in native routing mode.
#
# Creates pods on two different nodes and verifies:
#   1. ICMP ping between pods on different nodes
#   2. TCP connectivity via iperf3
#   3. BGP route advertisement via NovaRoute (PodCIDR visible in routing table)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 02: Cross-Node Connectivity (Native Routing)"

###############################################################################
# Prerequisite: check routing mode
###############################################################################
ROUTING_MODE="$(get_routing_mode)"
if [[ "$ROUTING_MODE" != "native" && "$ROUTING_MODE" != "unknown" ]]; then
    log_warn "Routing mode is '$ROUTING_MODE', not 'native'. Skipping test."
    begin_test "Native routing mode check"
    skip_test "Routing mode is '$ROUTING_MODE' — native routing not configured"
    print_summary
    exit 0
fi

###############################################################################
# Setup
###############################################################################
ensure_test_ns
register_cleanup

# Pick two distinct nodes
read -r NODE_A NODE_B <<< "$(pick_two_nodes)"
log_info "Node A: $NODE_A"
log_info "Node B: $NODE_B"

# Create pods on different nodes
create_pod "cross-native-a" "$NODE_A" "app=novanet-test,role=cross-a"
create_iperf3_server "cross-native-b" "$NODE_B"

wait_pod_ready "cross-native-a"
wait_pod_ready "cross-native-b"

POD_A_IP="$(get_pod_ip cross-native-a)"
POD_B_IP="$(get_pod_ip cross-native-b)"
POD_A_NODE="$(get_pod_node cross-native-a)"
POD_B_NODE="$(get_pod_node cross-native-b)"

log_info "Pod A: $POD_A_IP on $POD_A_NODE"
log_info "Pod B: $POD_B_IP on $POD_B_NODE"

if [[ "$POD_A_NODE" == "$POD_B_NODE" ]]; then
    log_fail "Both pods landed on the same node ($POD_A_NODE). Cannot test cross-node."
    exit 1
fi

###############################################################################
# Tests
###############################################################################

# Test 1: Ping A -> B (cross-node)
assert_ping "Ping A -> B (cross-node, native)" "cross-native-a" "$POD_B_IP"

# Test 2: Ping B -> A (cross-node, reverse direction)
assert_ping "Ping B -> A (cross-node, native)" "cross-native-b" "$POD_A_IP"

# Test 3: iperf3 TCP (A -> B)
begin_test "iperf3 TCP throughput (cross-node, native)"
sleep 2
IPERF_OUT="$(pod_exec cross-native-a iperf3 -c "$POD_B_IP" -t 5 -J 2>&1)" || true

if echo "$IPERF_OUT" | grep -q '"bits_per_second"'; then
    BPS="$(echo "$IPERF_OUT" | grep -o '"bits_per_second":[0-9.e+]*' | head -1 | cut -d: -f2)"
    MBPS="$(echo "$BPS" | awk '{printf "%.2f", $1/1000000}')"
    log_info "iperf3 throughput: ${MBPS} Mbps"
    pass_test
else
    log_info "iperf3 output: $(echo "$IPERF_OUT" | tail -5)"
    fail_test "iperf3 did not produce throughput data"
fi

# Test 4: Verify BGP route for the remote PodCIDR exists on each node.
# Extract the /24 PodCIDR from each pod IP (assumes /24 allocation).
begin_test "BGP route for remote PodCIDR on Node A"
POD_B_CIDR="$(echo "$POD_B_IP" | awk -F. '{print $1"."$2"."$3".0/24"}')"
log_info "Checking for route to $POD_B_CIDR on Node A..."

# Find the internal IP of Node A to SSH into it
NODE_A_IP="$(kubectl get node "$POD_A_NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')"

if [[ -n "$NODE_A_IP" ]]; then
    ROUTE_CHECK="$(ssh_node "$NODE_A_IP" "ip route show $POD_B_CIDR" 2>/dev/null || echo "")"
    if [[ -n "$ROUTE_CHECK" ]]; then
        log_info "Route found: $ROUTE_CHECK"
        pass_test
    else
        # Also check for a more specific route that covers the pod IP
        ROUTE_CHECK="$(ssh_node "$NODE_A_IP" "ip route get $POD_B_IP" 2>/dev/null || echo "")"
        if [[ -n "$ROUTE_CHECK" ]] && ! echo "$ROUTE_CHECK" | grep -q "unreachable"; then
            log_info "Route via: $ROUTE_CHECK"
            pass_test
        else
            fail_test "No route to $POD_B_CIDR found on $NODE_A_IP"
        fi
    fi
else
    skip_test "Could not determine internal IP of Node A"
fi

# Test 5: Verify no tunnel/overlay interface is used (native routing should not use geneve/vxlan).
begin_test "No overlay tunnel interface on Node A"
if [[ -n "$NODE_A_IP" ]]; then
    TUNNELS="$(ssh_node "$NODE_A_IP" "ip link show type geneve 2>/dev/null; ip link show type vxlan 2>/dev/null" 2>/dev/null || echo "")"
    # Filter out interfaces that are not related to NovaNet
    NOVANET_TUNNELS="$(echo "$TUNNELS" | grep -i "novanet\|nova" || echo "")"
    if [[ -z "$NOVANET_TUNNELS" ]]; then
        log_info "No NovaNet tunnel interfaces found (expected for native routing)"
        pass_test
    else
        log_warn "Found tunnel interfaces: $NOVANET_TUNNELS"
        fail_test "Overlay tunnel interfaces found in native routing mode"
    fi
else
    skip_test "Could not determine internal IP of Node A"
fi

###############################################################################
# Summary
###############################################################################
print_summary
