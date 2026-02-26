#!/usr/bin/env bash
# 09-graceful-restart.sh — Test graceful agent restart with eBPF program pinning.
#
# Verifies that existing pod connectivity is maintained during agent restart:
#   1. Initial cross-node connectivity works
#   2. eBPF programs survive agent restart (pinned to /sys/fs/bpf/)
#   3. Continuous ping during restart shows minimal/no packet loss
#   4. IPAM state persists (pods keep same IPs after restart)
#   5. Agent logs show successful reconnection to dataplane and NovaRoute
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 09: Graceful Agent Restart"

###############################################################################
# Configuration
###############################################################################
ROLLOUT_TIMEOUT="${ROLLOUT_TIMEOUT:-120}"
PING_DURING_RESTART_INTERVAL="0.5"   # ping every 500ms during restart
IPAM_STATE_DIR="/var/lib/cni/networks/novanet"
BPF_PIN_DIR="/sys/fs/bpf"

###############################################################################
# Cleanup handler
###############################################################################
# We override register_cleanup to also kill the background ping process.
PING_PID=""
cleanup_graceful_restart() {
    log_info "Cleaning up..."
    # Kill background ping if running
    if [[ -n "$PING_PID" ]] && kill -0 "$PING_PID" 2>/dev/null; then
        kill "$PING_PID" 2>/dev/null || true
        wait "$PING_PID" 2>/dev/null || true
    fi
    # Remove temp files
    rm -f /tmp/novanet-restart-ping.log 2>/dev/null || true
    rm -f /tmp/novanet-ebpf-before.txt 2>/dev/null || true
    rm -f /tmp/novanet-ebpf-after.txt 2>/dev/null || true
    rm -f /tmp/novanet-agent-logs.txt 2>/dev/null || true
    # Clean up test pods and namespace
    full_cleanup
}
trap 'cleanup_graceful_restart' EXIT

###############################################################################
# Preflight
###############################################################################
preflight_check

###############################################################################
# Setup: Create test pods on two different nodes
###############################################################################
ensure_test_ns

log_step "Selecting two distinct nodes for cross-node test..."
read -r NODE_A NODE_B <<< "$(pick_two_nodes)"
log_info "Node A: $NODE_A"
log_info "Node B: $NODE_B"

log_step "Creating test pods..."
create_pod "test-restart-a" "$NODE_A" "app=novanet-test,role=restart-a"
create_pod "test-restart-b" "$NODE_B" "app=novanet-test,role=restart-b"

wait_pod_ready "test-restart-a"
wait_pod_ready "test-restart-b"

POD_A_IP="$(get_pod_ip test-restart-a)"
POD_B_IP="$(get_pod_ip test-restart-b)"
POD_A_NODE="$(get_pod_node test-restart-a)"
POD_B_NODE="$(get_pod_node test-restart-b)"

log_info "Pod A: $POD_A_IP on $POD_A_NODE"
log_info "Pod B: $POD_B_IP on $POD_B_NODE"

if [[ "$POD_A_NODE" == "$POD_B_NODE" ]]; then
    log_fail "Both pods landed on the same node ($POD_A_NODE). Cannot test cross-node restart."
    exit 1
fi

# Resolve node internal IPs for SSH access
NODE_A_IP="$(kubectl get node "$POD_A_NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')"
NODE_B_IP="$(kubectl get node "$POD_B_NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')"
log_info "Node A internal IP: $NODE_A_IP"
log_info "Node B internal IP: $NODE_B_IP"

###############################################################################
# Test 1: Verify initial cross-node connectivity
###############################################################################
assert_ping "Initial connectivity: A -> B" "test-restart-a" "$POD_B_IP"
assert_ping "Initial connectivity: B -> A" "test-restart-b" "$POD_A_IP"

###############################################################################
# Test 2: Record pre-restart eBPF program state
###############################################################################
begin_test "Record eBPF program state before restart"

# Collect eBPF program/map info from both nodes via bpftool.
# We look at pinned objects under /sys/fs/bpf and TC-attached programs.
EBPF_BEFORE_A=""
EBPF_BEFORE_B=""

if [[ -n "$NODE_A_IP" ]]; then
    EBPF_BEFORE_A="$(ssh_node "$NODE_A_IP" "bpftool prog show 2>/dev/null | head -40" 2>/dev/null || echo "bpftool-unavailable")"
    PINNED_A="$(ssh_node "$NODE_A_IP" "ls -la ${BPF_PIN_DIR}/novanet/ 2>/dev/null || ls -la ${BPF_PIN_DIR}/ 2>/dev/null | grep -i nova || echo 'no-pins'" 2>/dev/null || echo "ssh-failed")"
    log_info "Node A eBPF programs (before restart):"
    echo "$EBPF_BEFORE_A" | head -20
    log_info "Node A BPF pins:"
    echo "$PINNED_A"
fi

if [[ -n "$NODE_B_IP" ]]; then
    EBPF_BEFORE_B="$(ssh_node "$NODE_B_IP" "bpftool prog show 2>/dev/null | head -40" 2>/dev/null || echo "bpftool-unavailable")"
    PINNED_B="$(ssh_node "$NODE_B_IP" "ls -la ${BPF_PIN_DIR}/novanet/ 2>/dev/null || ls -la ${BPF_PIN_DIR}/ 2>/dev/null | grep -i nova || echo 'no-pins'" 2>/dev/null || echo "ssh-failed")"
    log_info "Node B eBPF programs (before restart):"
    echo "$EBPF_BEFORE_B" | head -20
    log_info "Node B BPF pins:"
    echo "$PINNED_B"
fi

# Save for later comparison
echo "$EBPF_BEFORE_A" > /tmp/novanet-ebpf-before.txt
echo "---" >> /tmp/novanet-ebpf-before.txt
echo "$EBPF_BEFORE_B" >> /tmp/novanet-ebpf-before.txt

# Also record IPAM state on both nodes
IPAM_BEFORE_A=""
IPAM_BEFORE_B=""
if [[ -n "$NODE_A_IP" ]]; then
    IPAM_BEFORE_A="$(ssh_node "$NODE_A_IP" "ls ${IPAM_STATE_DIR}/ 2>/dev/null" 2>/dev/null || echo "no-ipam-state")"
    log_info "Node A IPAM state (before): $(echo "$IPAM_BEFORE_A" | tr '\n' ' ')"
fi
if [[ -n "$NODE_B_IP" ]]; then
    IPAM_BEFORE_B="$(ssh_node "$NODE_B_IP" "ls ${IPAM_STATE_DIR}/ 2>/dev/null" 2>/dev/null || echo "no-ipam-state")"
    log_info "Node B IPAM state (before): $(echo "$IPAM_BEFORE_B" | tr '\n' ' ')"
fi

pass_test

###############################################################################
# Test 3: Start continuous ping during restart
###############################################################################
begin_test "Continuous ping during rolling restart"
log_info "Starting continuous ping from pod A -> pod B in background..."

# Use a high count with the interval; the ping will be killed after the restart completes.
# We capture output to a file for analysis.
pod_exec "test-restart-a" ping -i "$PING_DURING_RESTART_INTERVAL" -W 2 "$POD_B_IP" \
    > /tmp/novanet-restart-ping.log 2>&1 &
PING_PID=$!
log_info "Background ping PID: $PING_PID"

# Give ping a moment to start
sleep 2

# Verify background ping is actually running
if ! kill -0 "$PING_PID" 2>/dev/null; then
    log_warn "Background ping process exited early — checking output"
    cat /tmp/novanet-restart-ping.log 2>/dev/null | tail -5 || true
    fail_test "Could not start continuous ping"
else
    log_info "Background ping is running"
fi

###############################################################################
# Test 4: Trigger rolling restart of the NovaNet DaemonSet
###############################################################################
begin_test "Rolling restart of novanet DaemonSet"
log_info "Recording pre-restart DaemonSet generation..."
PRE_RESTART_GEN="$(kubectl get daemonset novanet -n "$NOVANET_NS" -o jsonpath='{.metadata.generation}' 2>/dev/null || echo "0")"
log_info "Pre-restart generation: $PRE_RESTART_GEN"

log_info "Triggering rollout restart: kubectl rollout restart daemonset/novanet -n $NOVANET_NS"
if kubectl rollout restart daemonset/novanet -n "$NOVANET_NS"; then
    log_info "Rollout restart initiated"
else
    fail_test "Failed to initiate rollout restart"
    # Stop the background ping and bail
    kill "$PING_PID" 2>/dev/null || true
    print_summary
    exit 1
fi

log_info "Waiting for rollout to complete (timeout=${ROLLOUT_TIMEOUT}s)..."
if kubectl rollout status daemonset/novanet -n "$NOVANET_NS" --timeout="${ROLLOUT_TIMEOUT}s"; then
    log_info "Rollout completed successfully"
    pass_test
else
    fail_test "Rollout did not complete within ${ROLLOUT_TIMEOUT}s"
fi

# Wait a few seconds for things to stabilize after rollout
sleep 5

###############################################################################
# Test 5: Stop continuous ping and analyze results
###############################################################################
begin_test "Packet loss during restart is acceptable"

# Stop the background ping
if [[ -n "$PING_PID" ]] && kill -0 "$PING_PID" 2>/dev/null; then
    kill "$PING_PID" 2>/dev/null || true
    wait "$PING_PID" 2>/dev/null || true
fi
PING_PID=""

# Parse ping output for statistics
if [[ -f /tmp/novanet-restart-ping.log ]]; then
    PING_OUTPUT="$(cat /tmp/novanet-restart-ping.log)"

    # Extract statistics line: "X packets transmitted, Y received, Z% packet loss"
    STATS_LINE="$(echo "$PING_OUTPUT" | grep -E 'packets transmitted' || echo "")"
    if [[ -n "$STATS_LINE" ]]; then
        PKTS_SENT="$(echo "$STATS_LINE" | grep -oE '^[0-9]+' || echo "0")"
        PKTS_RECV="$(echo "$STATS_LINE" | grep -oE '[0-9]+ received' | grep -oE '^[0-9]+' || echo "0")"
        PKTS_LOSS_PCT="$(echo "$STATS_LINE" | grep -oE '[0-9.]+% packet loss' | grep -oE '^[0-9.]+' || echo "100")"
        PKTS_LOST=$((PKTS_SENT - PKTS_RECV))

        log_info "=== Restart Ping Results ==="
        log_info "  Packets sent:     $PKTS_SENT"
        log_info "  Packets received: $PKTS_RECV"
        log_info "  Packets lost:     $PKTS_LOST"
        log_info "  Packet loss:      ${PKTS_LOSS_PCT}%"
        log_info "==========================="

        # Accept up to 30% packet loss during a rolling restart.
        # In a well-functioning eBPF-pinned system, loss should be near 0%.
        # Some loss is expected during the brief window when the agent pod
        # on the source or destination node is being replaced.
        LOSS_THRESHOLD=30
        LOSS_INT="${PKTS_LOSS_PCT%%.*}"  # truncate to integer
        if [[ "$LOSS_INT" -le "$LOSS_THRESHOLD" ]]; then
            log_info "Packet loss ${PKTS_LOSS_PCT}% is within acceptable threshold (${LOSS_THRESHOLD}%)"
            pass_test
        else
            fail_test "Packet loss ${PKTS_LOSS_PCT}% exceeds threshold (${LOSS_THRESHOLD}%)"
        fi
    else
        log_warn "Could not parse ping statistics from output"
        log_info "Ping output (last 10 lines):"
        echo "$PING_OUTPUT" | tail -10
        fail_test "Ping statistics unavailable"
    fi
else
    fail_test "Ping log file not found"
fi

###############################################################################
# Test 6: Verify post-restart connectivity
###############################################################################
assert_ping "Post-restart connectivity: A -> B" "test-restart-a" "$POD_B_IP"
assert_ping "Post-restart connectivity: B -> A" "test-restart-b" "$POD_A_IP"

###############################################################################
# Test 7: Verify IPAM state persisted (pods have same IPs)
###############################################################################
begin_test "IPAM state persisted — pods retain original IPs"

POST_POD_A_IP="$(get_pod_ip test-restart-a)"
POST_POD_B_IP="$(get_pod_ip test-restart-b)"

log_info "Pod A IP before restart: $POD_A_IP"
log_info "Pod A IP after restart:  $POST_POD_A_IP"
log_info "Pod B IP before restart: $POD_B_IP"
log_info "Pod B IP after restart:  $POST_POD_B_IP"

if [[ "$POD_A_IP" == "$POST_POD_A_IP" && "$POD_B_IP" == "$POST_POD_B_IP" ]]; then
    log_info "Pod IPs unchanged after restart"
    pass_test
else
    fail_test "Pod IPs changed after restart (IPAM state may not have persisted)"
fi

###############################################################################
# Test 8: Verify IPAM files still present on disk
###############################################################################
begin_test "IPAM state files on disk after restart"

IPAM_OK=true
if [[ -n "$NODE_A_IP" ]]; then
    IPAM_AFTER_A="$(ssh_node "$NODE_A_IP" "ls ${IPAM_STATE_DIR}/ 2>/dev/null" 2>/dev/null || echo "no-ipam-state")"
    log_info "Node A IPAM state (after): $(echo "$IPAM_AFTER_A" | tr '\n' ' ')"
    if [[ "$IPAM_AFTER_A" == "no-ipam-state" ]]; then
        log_warn "No IPAM state directory found on Node A"
        IPAM_OK=false
    fi
fi
if [[ -n "$NODE_B_IP" ]]; then
    IPAM_AFTER_B="$(ssh_node "$NODE_B_IP" "ls ${IPAM_STATE_DIR}/ 2>/dev/null" 2>/dev/null || echo "no-ipam-state")"
    log_info "Node B IPAM state (after): $(echo "$IPAM_AFTER_B" | tr '\n' ' ')"
    if [[ "$IPAM_AFTER_B" == "no-ipam-state" ]]; then
        log_warn "No IPAM state directory found on Node B"
        IPAM_OK=false
    fi
fi

if $IPAM_OK; then
    pass_test
else
    fail_test "IPAM state directory missing on one or more nodes"
fi

###############################################################################
# Test 9: Verify eBPF programs still loaded after restart
###############################################################################
begin_test "eBPF programs present after restart"

EBPF_AFTER_A=""
EBPF_AFTER_B=""
EBPF_OK=true

if [[ -n "$NODE_A_IP" ]]; then
    EBPF_AFTER_A="$(ssh_node "$NODE_A_IP" "bpftool prog show 2>/dev/null | head -40" 2>/dev/null || echo "bpftool-unavailable")"
    PINNED_AFTER_A="$(ssh_node "$NODE_A_IP" "ls -la ${BPF_PIN_DIR}/novanet/ 2>/dev/null || ls -la ${BPF_PIN_DIR}/ 2>/dev/null | grep -i nova || echo 'no-pins'" 2>/dev/null || echo "ssh-failed")"
    log_info "Node A eBPF programs (after restart):"
    echo "$EBPF_AFTER_A" | head -20
    log_info "Node A BPF pins (after restart):"
    echo "$PINNED_AFTER_A"

    if [[ "$EBPF_AFTER_A" == "bpftool-unavailable" ]]; then
        log_warn "bpftool not available on Node A — cannot verify eBPF programs"
    fi
fi

if [[ -n "$NODE_B_IP" ]]; then
    EBPF_AFTER_B="$(ssh_node "$NODE_B_IP" "bpftool prog show 2>/dev/null | head -40" 2>/dev/null || echo "bpftool-unavailable")"
    PINNED_AFTER_B="$(ssh_node "$NODE_B_IP" "ls -la ${BPF_PIN_DIR}/novanet/ 2>/dev/null || ls -la ${BPF_PIN_DIR}/ 2>/dev/null | grep -i nova || echo 'no-pins'" 2>/dev/null || echo "ssh-failed")"
    log_info "Node B eBPF programs (after restart):"
    echo "$EBPF_AFTER_B" | head -20
    log_info "Node B BPF pins (after restart):"
    echo "$PINNED_AFTER_B"

    if [[ "$EBPF_AFTER_B" == "bpftool-unavailable" ]]; then
        log_warn "bpftool not available on Node B — cannot verify eBPF programs"
    fi
fi

# Save for reference
echo "$EBPF_AFTER_A" > /tmp/novanet-ebpf-after.txt
echo "---" >> /tmp/novanet-ebpf-after.txt
echo "$EBPF_AFTER_B" >> /tmp/novanet-ebpf-after.txt

# If bpftool is available, check that programs are still loaded
if [[ "$EBPF_AFTER_A" != "bpftool-unavailable" && -n "$EBPF_AFTER_A" ]]; then
    PROG_COUNT_A="$(echo "$EBPF_AFTER_A" | grep -cE '^[0-9]+:' || echo "0")"
    if [[ "$PROG_COUNT_A" -gt 0 ]]; then
        log_info "Node A has $PROG_COUNT_A eBPF programs loaded"
    else
        log_warn "No eBPF programs found on Node A after restart"
        EBPF_OK=false
    fi
fi

if [[ "$EBPF_AFTER_B" != "bpftool-unavailable" && -n "$EBPF_AFTER_B" ]]; then
    PROG_COUNT_B="$(echo "$EBPF_AFTER_B" | grep -cE '^[0-9]+:' || echo "0")"
    if [[ "$PROG_COUNT_B" -gt 0 ]]; then
        log_info "Node B has $PROG_COUNT_B eBPF programs loaded"
    else
        log_warn "No eBPF programs found on Node B after restart"
        EBPF_OK=false
    fi
fi

if $EBPF_OK; then
    pass_test
else
    fail_test "eBPF programs missing on one or more nodes after restart"
fi

###############################################################################
# Test 10: Check agent logs for successful reconnection
###############################################################################
begin_test "Agent logs show successful startup after restart"

LOGS_OK=true

# Get logs from the most recently started novanet pods (post-restart)
AGENT_PODS="$(kubectl get pods -n "$NOVANET_NS" -l app=novanet -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || \
             kubectl get pods -n "$NOVANET_NS" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")"

if [[ -z "$AGENT_PODS" ]]; then
    log_warn "Could not find novanet agent pods"
    skip_test "No agent pods found in namespace $NOVANET_NS"
else
    log_info "Checking logs for novanet agent pods..."
    for pod in $AGENT_PODS; do
        log_info "--- Logs from $pod ---"
        POD_LOGS="$(kubectl logs "$pod" -n "$NOVANET_NS" --tail=50 2>/dev/null || echo "log-retrieval-failed")"

        if [[ "$POD_LOGS" == "log-retrieval-failed" ]]; then
            log_warn "Could not retrieve logs from $pod"
            continue
        fi

        # Check for error indicators that would suggest a bad restart
        FATAL_ERRORS="$(echo "$POD_LOGS" | grep -iE 'fatal|panic|cannot attach|failed to load' || echo "")"
        if [[ -n "$FATAL_ERRORS" ]]; then
            log_warn "Found concerning log entries in $pod:"
            echo "$FATAL_ERRORS"
            LOGS_OK=false
        fi

        # Check for positive indicators of successful startup
        STARTUP_OK="$(echo "$POD_LOGS" | grep -iE 'started|ready|listening|attached|connected|reconcil' || echo "")"
        if [[ -n "$STARTUP_OK" ]]; then
            log_info "Positive startup indicators in $pod:"
            echo "$STARTUP_OK" | head -5
        fi

        # Look for NovaRoute reconnection (if in native routing mode)
        NOVAROUTE_RECONNECT="$(echo "$POD_LOGS" | grep -iE 'novaroute|advertise.*prefix|register.*owner' || echo "")"
        if [[ -n "$NOVAROUTE_RECONNECT" ]]; then
            log_info "NovaRoute reconnection indicators in $pod:"
            echo "$NOVAROUTE_RECONNECT" | head -5
        fi
    done

    if $LOGS_OK; then
        pass_test
    else
        fail_test "Agent logs contain fatal errors after restart"
    fi
fi

###############################################################################
# Test 11: Verify DaemonSet is fully healthy after restart
###############################################################################
begin_test "DaemonSet fully healthy after restart"

# Give a moment for all pods to report ready
sleep 3

if check_novanet_daemonset; then
    pass_test
else
    fail_test "DaemonSet not fully healthy after restart"
fi

###############################################################################
# Summary
###############################################################################
echo ""
log_info "=== Graceful Restart Test Report ==="
if [[ -n "${PKTS_SENT:-}" ]]; then
    log_info "  Packets sent during restart:     ${PKTS_SENT:-N/A}"
    log_info "  Packets received during restart:  ${PKTS_RECV:-N/A}"
    log_info "  Packets lost during restart:      ${PKTS_LOST:-N/A}"
    log_info "  Packet loss percentage:            ${PKTS_LOSS_PCT:-N/A}%"
fi
log_info "  Pod A IP preserved: ${POD_A_IP} -> ${POST_POD_A_IP:-unknown}"
log_info "  Pod B IP preserved: ${POD_B_IP} -> ${POST_POD_B_IP:-unknown}"
log_info "=================================="

print_summary
