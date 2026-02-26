#!/usr/bin/env bash
# 03-cross-node-geneve.sh — Test cross-node pod-to-pod connectivity via Geneve overlay.
#
# Verifies:
#   1. Geneve tunnel interface exists on nodes
#   2. ICMP ping between pods on different nodes (over Geneve)
#   3. TCP connectivity via iperf3 over Geneve
#   4. Geneve TLV identity header is carried (if observable)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 03: Cross-Node Connectivity (Geneve Overlay)"

###############################################################################
# Prerequisite: check routing mode and tunnel protocol
###############################################################################
ROUTING_MODE="$(get_routing_mode)"
TUNNEL_PROTO="$(get_tunnel_protocol)"

if [[ "$ROUTING_MODE" == "native" ]]; then
    log_warn "Routing mode is 'native' — Geneve overlay test requires 'overlay' mode."
    begin_test "Geneve overlay mode check"
    skip_test "Routing mode is 'native'. Set routingMode=overlay and tunnelProtocol=geneve to run this test."
    print_summary
    exit 0
fi

if [[ "$TUNNEL_PROTO" != "geneve" && "$TUNNEL_PROTO" != "unknown" ]]; then
    log_warn "Tunnel protocol is '$TUNNEL_PROTO', not 'geneve'."
    begin_test "Geneve tunnel protocol check"
    skip_test "Tunnel protocol is '$TUNNEL_PROTO'. Set tunnelProtocol=geneve to run this test."
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

create_pod "cross-geneve-a" "$NODE_A" "app=novanet-test,role=geneve-a"
create_iperf3_server "cross-geneve-b" "$NODE_B"

wait_pod_ready "cross-geneve-a"
wait_pod_ready "cross-geneve-b"

POD_A_IP="$(get_pod_ip cross-geneve-a)"
POD_B_IP="$(get_pod_ip cross-geneve-b)"
POD_A_NODE="$(get_pod_node cross-geneve-a)"
POD_B_NODE="$(get_pod_node cross-geneve-b)"

log_info "Pod A: $POD_A_IP on $POD_A_NODE"
log_info "Pod B: $POD_B_IP on $POD_B_NODE"

if [[ "$POD_A_NODE" == "$POD_B_NODE" ]]; then
    log_fail "Both pods landed on the same node ($POD_A_NODE). Cannot test cross-node."
    exit 1
fi

# Resolve node internal IPs for SSH
NODE_A_IP="$(kubectl get node "$POD_A_NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')"
NODE_B_IP="$(kubectl get node "$POD_B_NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')"

###############################################################################
# Tests
###############################################################################

# Test 1: Verify Geneve interface exists on Node A
begin_test "Geneve tunnel interface exists on Node A"
if [[ -n "$NODE_A_IP" ]]; then
    GENEVE_IFACE="$(ssh_node "$NODE_A_IP" "ip link show type geneve 2>/dev/null" || echo "")"
    if [[ -n "$GENEVE_IFACE" ]]; then
        log_info "Geneve interface: $(echo "$GENEVE_IFACE" | head -1)"
        pass_test
    else
        fail_test "No Geneve interface found on Node A ($NODE_A_IP)"
    fi
else
    skip_test "Could not resolve Node A IP"
fi

# Test 2: Verify Geneve interface exists on Node B
begin_test "Geneve tunnel interface exists on Node B"
if [[ -n "$NODE_B_IP" ]]; then
    GENEVE_IFACE="$(ssh_node "$NODE_B_IP" "ip link show type geneve 2>/dev/null" || echo "")"
    if [[ -n "$GENEVE_IFACE" ]]; then
        log_info "Geneve interface: $(echo "$GENEVE_IFACE" | head -1)"
        pass_test
    else
        fail_test "No Geneve interface found on Node B ($NODE_B_IP)"
    fi
else
    skip_test "Could not resolve Node B IP"
fi

# Test 3: Verify Geneve port (6081/udp) is listening
begin_test "Geneve port 6081/udp listening on Node A"
if [[ -n "$NODE_A_IP" ]]; then
    PORT_CHECK="$(ssh_node "$NODE_A_IP" "ss -ulnp | grep 6081" 2>/dev/null || echo "")"
    if [[ -n "$PORT_CHECK" ]]; then
        log_info "Port 6081: $PORT_CHECK"
        pass_test
    else
        fail_test "Geneve port 6081/udp not listening on Node A"
    fi
else
    skip_test "Could not resolve Node A IP"
fi

# Test 4: Ping A -> B (cross-node over Geneve)
assert_ping "Ping A -> B (cross-node, Geneve)" "cross-geneve-a" "$POD_B_IP"

# Test 5: Ping B -> A (reverse)
assert_ping "Ping B -> A (cross-node, Geneve)" "cross-geneve-b" "$POD_A_IP"

# Test 6: iperf3 TCP over Geneve
begin_test "iperf3 TCP throughput (cross-node, Geneve)"
sleep 2
IPERF_OUT="$(pod_exec cross-geneve-a iperf3 -c "$POD_B_IP" -t 5 -J 2>&1)" || true

if echo "$IPERF_OUT" | grep -q '"bits_per_second"'; then
    BPS="$(echo "$IPERF_OUT" | grep -o '"bits_per_second":[0-9.e+]*' | head -1 | cut -d: -f2)"
    MBPS="$(echo "$BPS" | awk '{printf "%.2f", $1/1000000}')"
    log_info "iperf3 throughput: ${MBPS} Mbps"
    pass_test
else
    log_info "iperf3 output: $(echo "$IPERF_OUT" | tail -5)"
    fail_test "iperf3 did not produce throughput data"
fi

# Test 7: Capture a packet on the Geneve interface to verify encapsulation
begin_test "Geneve encapsulation observed (tcpdump)"
if [[ -n "$NODE_A_IP" ]]; then
    # Start a background tcpdump capturing Geneve-encapsulated traffic (UDP 6081)
    # while we send a ping
    ssh_node "$NODE_A_IP" "timeout 10 tcpdump -c 5 -i any udp port 6081 -w /tmp/geneve-test.pcap &>/dev/null &" 2>/dev/null || true
    sleep 1

    # Send traffic to trigger encapsulated packets
    pod_exec cross-geneve-a ping -c 3 -W 5 "$POD_B_IP" &>/dev/null || true
    sleep 3

    # Check if we captured any Geneve packets
    PCAP_COUNT="$(ssh_node "$NODE_A_IP" "tcpdump -r /tmp/geneve-test.pcap 2>/dev/null | wc -l" 2>/dev/null || echo "0")"
    ssh_node "$NODE_A_IP" "rm -f /tmp/geneve-test.pcap" 2>/dev/null || true

    if [[ "$PCAP_COUNT" -gt 0 ]]; then
        log_info "Captured $PCAP_COUNT Geneve-encapsulated packets"
        pass_test
    else
        fail_test "No Geneve-encapsulated packets captured (may need tcpdump installed)"
    fi
else
    skip_test "Could not resolve Node A IP"
fi

###############################################################################
# Summary
###############################################################################
print_summary
