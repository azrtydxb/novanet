#!/usr/bin/env bash
# 04-cross-node-vxlan.sh — Test cross-node pod-to-pod connectivity via VXLAN overlay.
#
# Verifies:
#   1. VXLAN tunnel interface exists on nodes
#   2. ICMP ping between pods on different nodes (over VXLAN)
#   3. TCP connectivity via iperf3 over VXLAN
#   4. VXLAN encapsulation on port 4789
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 04: Cross-Node Connectivity (VXLAN Overlay)"

###############################################################################
# Prerequisite: check routing mode and tunnel protocol
###############################################################################
ROUTING_MODE="$(get_routing_mode)"
TUNNEL_PROTO="$(get_tunnel_protocol)"

if [[ "$ROUTING_MODE" == "native" ]]; then
    log_warn "Routing mode is 'native' — VXLAN overlay test requires 'overlay' mode."
    begin_test "VXLAN overlay mode check"
    skip_test "Routing mode is 'native'. Set routingMode=overlay and tunnelProtocol=vxlan to run this test."
    print_summary
    exit 0
fi

if [[ "$TUNNEL_PROTO" != "vxlan" && "$TUNNEL_PROTO" != "unknown" ]]; then
    log_warn "Tunnel protocol is '$TUNNEL_PROTO', not 'vxlan'."
    begin_test "VXLAN tunnel protocol check"
    skip_test "Tunnel protocol is '$TUNNEL_PROTO'. Set tunnelProtocol=vxlan to run this test."
    print_summary
    exit 0
fi

###############################################################################
# Setup
###############################################################################
ensure_test_ns
register_cleanup

read -r NODE_A NODE_B <<< "$(pick_two_nodes)"
log_info "Node A: $NODE_A"
log_info "Node B: $NODE_B"

create_pod "cross-vxlan-a" "$NODE_A" "app=novanet-test,role=vxlan-a"
create_iperf3_server "cross-vxlan-b" "$NODE_B"

wait_pod_ready "cross-vxlan-a"
wait_pod_ready "cross-vxlan-b"

POD_A_IP="$(get_pod_ip cross-vxlan-a)"
POD_B_IP="$(get_pod_ip cross-vxlan-b)"
POD_A_NODE="$(get_pod_node cross-vxlan-a)"
POD_B_NODE="$(get_pod_node cross-vxlan-b)"

log_info "Pod A: $POD_A_IP on $POD_A_NODE"
log_info "Pod B: $POD_B_IP on $POD_B_NODE"

if [[ "$POD_A_NODE" == "$POD_B_NODE" ]]; then
    log_fail "Both pods landed on the same node ($POD_A_NODE). Cannot test cross-node."
    exit 1
fi

NODE_A_IP="$(kubectl get node "$POD_A_NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')"
NODE_B_IP="$(kubectl get node "$POD_B_NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')"

###############################################################################
# Tests
###############################################################################

# Test 1: Verify VXLAN interface exists on Node A
begin_test "VXLAN tunnel interface exists on Node A"
if [[ -n "$NODE_A_IP" ]]; then
    VXLAN_IFACE="$(ssh_node "$NODE_A_IP" "ip link show type vxlan 2>/dev/null" || echo "")"
    if [[ -n "$VXLAN_IFACE" ]]; then
        log_info "VXLAN interface: $(echo "$VXLAN_IFACE" | head -1)"
        pass_test
    else
        fail_test "No VXLAN interface found on Node A ($NODE_A_IP)"
    fi
else
    skip_test "Could not resolve Node A IP"
fi

# Test 2: Verify VXLAN interface exists on Node B
begin_test "VXLAN tunnel interface exists on Node B"
if [[ -n "$NODE_B_IP" ]]; then
    VXLAN_IFACE="$(ssh_node "$NODE_B_IP" "ip link show type vxlan 2>/dev/null" || echo "")"
    if [[ -n "$VXLAN_IFACE" ]]; then
        log_info "VXLAN interface: $(echo "$VXLAN_IFACE" | head -1)"
        pass_test
    else
        fail_test "No VXLAN interface found on Node B ($NODE_B_IP)"
    fi
else
    skip_test "Could not resolve Node B IP"
fi

# Test 3: Verify VXLAN port (4789/udp) is listening
begin_test "VXLAN port 4789/udp listening on Node A"
if [[ -n "$NODE_A_IP" ]]; then
    PORT_CHECK="$(ssh_node "$NODE_A_IP" "ss -ulnp | grep 4789" 2>/dev/null || echo "")"
    if [[ -n "$PORT_CHECK" ]]; then
        log_info "Port 4789: $PORT_CHECK"
        pass_test
    else
        fail_test "VXLAN port 4789/udp not listening on Node A"
    fi
else
    skip_test "Could not resolve Node A IP"
fi

# Test 4: Ping A -> B (cross-node over VXLAN)
assert_ping "Ping A -> B (cross-node, VXLAN)" "cross-vxlan-a" "$POD_B_IP"

# Test 5: Ping B -> A (reverse)
assert_ping "Ping B -> A (cross-node, VXLAN)" "cross-vxlan-b" "$POD_A_IP"

# Test 6: iperf3 TCP over VXLAN
begin_test "iperf3 TCP throughput (cross-node, VXLAN)"
sleep 2
IPERF_OUT="$(pod_exec cross-vxlan-a iperf3 -c "$POD_B_IP" -t 5 -J 2>&1)" || true

if echo "$IPERF_OUT" | grep -q '"bits_per_second"'; then
    BPS="$(echo "$IPERF_OUT" | grep -o '"bits_per_second":[0-9.e+]*' | head -1 | cut -d: -f2)"
    MBPS="$(echo "$BPS" | awk '{printf "%.2f", $1/1000000}')"
    log_info "iperf3 throughput: ${MBPS} Mbps"
    pass_test
else
    log_info "iperf3 output: $(echo "$IPERF_OUT" | tail -5)"
    fail_test "iperf3 did not produce throughput data"
fi

# Test 7: Capture VXLAN-encapsulated packets on UDP port 4789
begin_test "VXLAN encapsulation observed (tcpdump)"
if [[ -n "$NODE_A_IP" ]]; then
    ssh_node "$NODE_A_IP" "timeout 10 tcpdump -c 5 -i any udp port 4789 -w /tmp/vxlan-test.pcap &>/dev/null &" 2>/dev/null || true
    sleep 1

    pod_exec cross-vxlan-a ping -c 3 -W 5 "$POD_B_IP" &>/dev/null || true
    sleep 3

    PCAP_COUNT="$(ssh_node "$NODE_A_IP" "tcpdump -r /tmp/vxlan-test.pcap 2>/dev/null | wc -l" 2>/dev/null || echo "0")"
    ssh_node "$NODE_A_IP" "rm -f /tmp/vxlan-test.pcap" 2>/dev/null || true

    if [[ "$PCAP_COUNT" -gt 0 ]]; then
        log_info "Captured $PCAP_COUNT VXLAN-encapsulated packets"
        pass_test
    else
        fail_test "No VXLAN-encapsulated packets captured"
    fi
else
    skip_test "Could not resolve Node A IP"
fi

# Test 8: Verify VXLAN VNI is set (check the interface details)
begin_test "VXLAN VNI configured on tunnel interface"
if [[ -n "$NODE_A_IP" ]]; then
    VNI_INFO="$(ssh_node "$NODE_A_IP" "ip -d link show type vxlan 2>/dev/null | grep -i 'vxlan id'" || echo "")"
    if [[ -n "$VNI_INFO" ]]; then
        log_info "VXLAN VNI info: $VNI_INFO"
        pass_test
    else
        fail_test "Could not determine VXLAN VNI"
    fi
else
    skip_test "Could not resolve Node A IP"
fi

###############################################################################
# Summary
###############################################################################
print_summary
