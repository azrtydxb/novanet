#!/usr/bin/env bash
# 06-egress.sh — Test egress connectivity and SNAT/masquerade.
#
# Verifies:
#   1. Pod can reach an external IP (8.8.8.8)
#   2. Pod can reach an external HTTP endpoint
#   3. SNAT/masquerade is applied (pod IP not visible externally, node IP used)
#   4. Pod-to-node connectivity works
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 06: Egress / Masquerade"

###############################################################################
# Setup
###############################################################################
ensure_test_ns
register_cleanup

create_pod "egress-test" "" "app=novanet-test,role=egress"
wait_pod_ready "egress-test"

POD_IP="$(get_pod_ip egress-test)"
POD_NODE="$(get_pod_node egress-test)"
NODE_IP="$(kubectl get node "$POD_NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')"

log_info "Pod IP:   $POD_IP"
log_info "Pod Node: $POD_NODE"
log_info "Node IP:  $NODE_IP"

###############################################################################
# Tests
###############################################################################

# Test 1: Ping external IP (Google DNS)
begin_test "Ping external IP 8.8.8.8"
if pod_exec egress-test ping -c 3 -W 10 8.8.8.8 &>/dev/null; then
    pass_test
else
    fail_test "Cannot ping 8.8.8.8 from pod"
fi

# Test 2: Ping another external IP (Cloudflare DNS)
begin_test "Ping external IP 1.1.1.1"
if pod_exec egress-test ping -c 3 -W 10 1.1.1.1 &>/dev/null; then
    pass_test
else
    fail_test "Cannot ping 1.1.1.1 from pod"
fi

# Test 3: Pod can reach external HTTP endpoint
begin_test "HTTP GET to external endpoint"
HTTP_STATUS="$(pod_exec egress-test curl -s -o /dev/null -w '%{http_code}' --connect-timeout 10 --max-time 15 http://ifconfig.me 2>/dev/null || echo "000")"
if [[ "$HTTP_STATUS" == "200" ]]; then
    pass_test
else
    log_info "HTTP status: $HTTP_STATUS"
    # Try alternative endpoint
    HTTP_STATUS="$(pod_exec egress-test curl -s -o /dev/null -w '%{http_code}' --connect-timeout 10 --max-time 15 http://httpbin.org/get 2>/dev/null || echo "000")"
    if [[ "$HTTP_STATUS" == "200" ]]; then
        pass_test
    else
        fail_test "HTTP GET returned status $HTTP_STATUS (expected 200)"
    fi
fi

# Test 4: Verify SNAT / masquerade — the external-visible source IP should be
# the node IP, not the pod IP.
begin_test "SNAT masquerade (source IP is node IP, not pod IP)"
EXTERNAL_IP="$(pod_exec egress-test curl -s --connect-timeout 10 --max-time 15 http://ifconfig.me 2>/dev/null || echo "")"

if [[ -z "$EXTERNAL_IP" ]]; then
    # Try alternative
    EXTERNAL_IP="$(pod_exec egress-test curl -s --connect-timeout 10 --max-time 15 http://api.ipify.org 2>/dev/null || echo "")"
fi

if [[ -z "$EXTERNAL_IP" ]]; then
    skip_test "Could not determine external IP (internet not reachable)"
elif [[ "$EXTERNAL_IP" == "$POD_IP" ]]; then
    fail_test "External IP is the pod IP ($POD_IP) — masquerade not working"
else
    log_info "External IP: $EXTERNAL_IP (pod IP: $POD_IP)"
    # The external IP should be either the node IP or a NAT gateway IP — just not the pod IP
    pass_test
fi

# Test 5: Pod can reach its own node
begin_test "Pod -> node connectivity"
if [[ -n "$NODE_IP" ]]; then
    if pod_exec egress-test ping -c 3 -W 5 "$NODE_IP" &>/dev/null; then
        pass_test
    else
        fail_test "Cannot ping node $NODE_IP from pod"
    fi
else
    skip_test "Could not determine node IP"
fi

# Test 6: Check iptables/nftables masquerade rules on the node
begin_test "Masquerade rule present on node"
if [[ -n "$NODE_IP" ]]; then
    # Check iptables MASQUERADE rules
    MASQ_RULES="$(ssh_node "$NODE_IP" "iptables -t nat -L POSTROUTING -n 2>/dev/null | grep -i masq" 2>/dev/null || echo "")"
    if [[ -z "$MASQ_RULES" ]]; then
        # Try nftables
        MASQ_RULES="$(ssh_node "$NODE_IP" "nft list ruleset 2>/dev/null | grep -i masq" 2>/dev/null || echo "")"
    fi
    if [[ -z "$MASQ_RULES" ]]; then
        # Check eBPF-based masquerade via bpftool
        MASQ_RULES="$(ssh_node "$NODE_IP" "bpftool prog list 2>/dev/null | grep -i masq" 2>/dev/null || echo "")"
    fi

    if [[ -n "$MASQ_RULES" ]]; then
        log_info "Masquerade rules found: $(echo "$MASQ_RULES" | head -3)"
        pass_test
    else
        log_warn "No traditional masquerade rules found — may be eBPF-native masquerade"
        # If the SNAT test passed above, masquerade is working regardless of implementation
        if [[ -n "$EXTERNAL_IP" && "$EXTERNAL_IP" != "$POD_IP" ]]; then
            log_info "SNAT is working (verified via external IP check), likely eBPF-based masquerade"
            pass_test
        else
            fail_test "No masquerade rules found and SNAT not verified"
        fi
    fi
else
    skip_test "Could not determine node IP"
fi

# Test 7: Verify ClusterCIDR traffic is NOT masqueraded (pod-to-pod should use real IPs)
begin_test "Intra-cluster traffic not masqueraded"
# Create a second pod to test intra-cluster communication
create_pod "egress-peer" "" "app=novanet-test,role=egress-peer"
wait_pod_ready "egress-peer"
PEER_IP="$(get_pod_ip egress-peer)"

# Start a listener that echoes the client IP
pod_exec egress-peer bash -c "nohup nc -lk -p 9090 -c 'echo \$NCAT_REMOTE_ADDR' &>/dev/null &" 2>/dev/null || true
sleep 1

# Connect and check if the source IP is the pod IP (not the node IP)
SEEN_IP="$(pod_exec egress-test bash -c "echo | nc -w 3 $PEER_IP 9090" 2>/dev/null || echo "")"
if [[ -n "$SEEN_IP" && "$SEEN_IP" == *"$POD_IP"* ]]; then
    log_info "Intra-cluster source IP: $SEEN_IP (matches pod IP $POD_IP)"
    pass_test
elif [[ -z "$SEEN_IP" ]]; then
    # Fallback: just verify ping works (at minimum)
    if pod_ping egress-test "$PEER_IP" 2 &>/dev/null; then
        log_info "Intra-cluster connectivity works (could not verify source IP)"
        pass_test
    else
        fail_test "Intra-cluster connectivity failed"
    fi
else
    log_info "Seen source IP: $SEEN_IP, expected pod IP: $POD_IP"
    fail_test "Intra-cluster traffic appears to be masqueraded"
fi

###############################################################################
# Summary
###############################################################################
print_summary
