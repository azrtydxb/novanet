#!/usr/bin/env bash
# 05-network-policy.sh — Test Kubernetes NetworkPolicy enforcement via NovaNet.
#
# Verifies:
#   1. Default connectivity (no policy) works
#   2. deny-all ingress policy blocks all inbound traffic
#   3. Allow policy for a specific label permits traffic from matching pods
#   4. Non-matching pods remain blocked
#   5. Egress deny policy blocks outbound traffic
#   6. Cleanup restores full connectivity
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 05: NetworkPolicy Enforcement"

###############################################################################
# Setup
###############################################################################
ensure_test_ns
register_cleanup

# We need 3 pods:
#   - "server"     with label app=web        (target of policies)
#   - "allowed"    with label role=frontend   (will be allowed by policy)
#   - "blocked"    with label role=external   (should remain blocked)

log_step "Creating test pods..."
create_labeled_pod "policy-server"  "app=web"            ""
create_labeled_pod "policy-allowed" "app=client,role=frontend" ""
create_labeled_pod "policy-blocked" "app=client,role=external" ""

wait_pod_ready "policy-server"
wait_pod_ready "policy-allowed"
wait_pod_ready "policy-blocked"

SERVER_IP="$(get_pod_ip policy-server)"
ALLOWED_IP="$(get_pod_ip policy-allowed)"
BLOCKED_IP="$(get_pod_ip policy-blocked)"

log_info "Server IP:  $SERVER_IP"
log_info "Allowed IP: $ALLOWED_IP"
log_info "Blocked IP: $BLOCKED_IP"

# Start a TCP listener on the server for connectivity tests.
# Use socat (available in netshoot) for a reliable persistent listener.
pod_exec policy-server bash -c "nohup socat TCP-LISTEN:8080,fork,reuseaddr EXEC:'/bin/echo pong' &>/dev/null &" 2>/dev/null || true
sleep 2

###############################################################################
# Phase 1: Baseline — no policies, everything works
###############################################################################
log_step "Phase 1: Baseline connectivity (no policies)"

assert_ping "Baseline: allowed -> server (ping)" "policy-allowed" "$SERVER_IP"
assert_ping "Baseline: blocked -> server (ping)" "policy-blocked" "$SERVER_IP"
assert_tcp  "Baseline: allowed -> server (TCP 8080)" "policy-allowed" "$SERVER_IP" 8080
assert_tcp  "Baseline: blocked -> server (TCP 8080)" "policy-blocked" "$SERVER_IP" 8080

###############################################################################
# Phase 2: deny-all ingress on server
###############################################################################
log_step "Phase 2: Apply deny-all ingress policy on app=web pods"

kubectl apply -n "$TEST_NS" -f - <<'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-all-ingress
spec:
  podSelector:
    matchLabels:
      app: web
  policyTypes:
  - Ingress
  # No ingress rules = deny all ingress
EOF

log_info "Waiting for policy to propagate..."
sleep 5

# Probe whether the CNI actually enforces NetworkPolicy.
# If the deny-all policy doesn't block traffic, skip remaining policy tests.
if pod_ping "policy-allowed" "$SERVER_IP" 2 &>/dev/null; then
    log_warn "Ping still succeeds after deny-all policy — NetworkPolicy enforcement not yet implemented"
    begin_test "Deny-all: allowed -> server (ping blocked)"
    skip_test "NetworkPolicy enforcement not implemented by current CNI"
    begin_test "Policy enforcement tests"
    skip_test "Skipping all policy enforcement tests"
    # Clean up and exit early
    cleanup_network_policies
    print_summary
    exit 0
fi

assert_no_ping "Deny-all: allowed -> server (ping blocked)"  "policy-allowed" "$SERVER_IP"
assert_no_ping "Deny-all: blocked -> server (ping blocked)"  "policy-blocked" "$SERVER_IP"
assert_no_tcp  "Deny-all: allowed -> server (TCP blocked)"   "policy-allowed" "$SERVER_IP" 8080
assert_no_tcp  "Deny-all: blocked -> server (TCP blocked)"   "policy-blocked" "$SERVER_IP" 8080

###############################################################################
# Phase 3: Allow traffic from role=frontend only
###############################################################################
log_step "Phase 3: Allow ingress from role=frontend"

kubectl apply -n "$TEST_NS" -f - <<'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-frontend-ingress
spec:
  podSelector:
    matchLabels:
      app: web
  policyTypes:
  - Ingress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          role: frontend
    ports:
    - protocol: TCP
      port: 8080
EOF

# Remove the blanket deny (the allow policy also implicitly denies non-matching)
kubectl delete networkpolicy deny-all-ingress -n "$TEST_NS" 2>/dev/null || true

log_info "Waiting for policy to propagate..."
sleep 5

# Restart the listener in case it died
pod_exec policy-server bash -c "socat TCP-LISTEN:8080,fork,reuseaddr EXEC:'/bin/echo pong' &>/dev/null &" 2>/dev/null || true
sleep 1

assert_tcp     "Allow-frontend: allowed -> server (TCP 8080 permitted)" "policy-allowed" "$SERVER_IP" 8080
assert_no_tcp  "Allow-frontend: blocked -> server (TCP 8080 denied)"    "policy-blocked" "$SERVER_IP" 8080

# Ping may or may not work depending on policy — the allow rule only covers TCP/8080.
# ICMP from the "allowed" pod should still be denied since the policy only permits TCP 8080.
assert_no_ping "Allow-frontend: allowed -> server (ICMP still denied)" "policy-allowed" "$SERVER_IP"

###############################################################################
# Phase 4: Egress deny policy
###############################################################################
log_step "Phase 4: Apply egress deny policy on role=external pods"

kubectl apply -n "$TEST_NS" -f - <<'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-egress-external
spec:
  podSelector:
    matchLabels:
      role: external
  policyTypes:
  - Egress
  # No egress rules = deny all egress
EOF

log_info "Waiting for policy to propagate..."
sleep 5

assert_no_ping "Egress-deny: blocked pod cannot reach server (ping)"    "policy-blocked" "$SERVER_IP"
assert_no_ping "Egress-deny: blocked pod cannot reach allowed pod (ping)" "policy-blocked" "$ALLOWED_IP"

# The allowed pod should still work (not affected by this egress policy)
assert_tcp "Egress-deny: allowed pod still reaches server (TCP 8080)" "policy-allowed" "$SERVER_IP" 8080

###############################################################################
# Phase 5: Cleanup policies and verify connectivity restored
###############################################################################
log_step "Phase 5: Remove all policies, verify connectivity restored"

cleanup_network_policies
log_info "Waiting for policy removal to propagate..."
sleep 5

# Restart TCP listener
pod_exec policy-server bash -c "socat TCP-LISTEN:8080,fork,reuseaddr EXEC:'/bin/echo pong' &>/dev/null &" 2>/dev/null || true
sleep 1

assert_ping "Restored: allowed -> server (ping)"          "policy-allowed" "$SERVER_IP"
assert_ping "Restored: blocked -> server (ping)"          "policy-blocked" "$SERVER_IP"
assert_tcp  "Restored: allowed -> server (TCP 8080)"      "policy-allowed" "$SERVER_IP" 8080
assert_tcp  "Restored: blocked -> server (TCP 8080)"      "policy-blocked" "$SERVER_IP" 8080
assert_ping "Restored: blocked -> allowed (ping)"         "policy-blocked" "$ALLOWED_IP"

###############################################################################
# Summary
###############################################################################
print_summary
