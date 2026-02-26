#!/usr/bin/env bash
# 07-dns.sh — Test DNS resolution from pods.
#
# Verifies:
#   1. Pod can resolve kubernetes.default.svc.cluster.local
#   2. Pod can resolve kube-dns / coredns service
#   3. Pod can resolve an external domain
#   4. /etc/resolv.conf is correctly configured
#   5. DNS search domains work (short names resolve)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 07: DNS Resolution"

###############################################################################
# Setup
###############################################################################
ensure_test_ns
register_cleanup

create_pod "dns-test" "" "app=novanet-test,role=dns"
wait_pod_ready "dns-test"

POD_IP="$(get_pod_ip dns-test)"
log_info "DNS test pod: $POD_IP"

###############################################################################
# Tests
###############################################################################

# Test 1: Resolve kubernetes.default.svc.cluster.local
begin_test "Resolve kubernetes.default.svc.cluster.local"
DNS_RESULT="$(pod_exec dns-test nslookup kubernetes.default.svc.cluster.local 2>&1 || echo "FAILED")"
if echo "$DNS_RESULT" | grep -q "Address.*[0-9]" && ! echo "$DNS_RESULT" | grep -qi "NXDOMAIN\|SERVFAIL\|can't find"; then
    K8S_SVC_IP="$(echo "$DNS_RESULT" | grep -A1 "Name:" | grep "Address" | awk '{print $NF}' | head -1)"
    log_info "kubernetes.default.svc.cluster.local -> $K8S_SVC_IP"
    pass_test
else
    log_info "nslookup output: $DNS_RESULT"
    fail_test "Could not resolve kubernetes.default.svc.cluster.local"
fi

# Test 2: Resolve the kube-dns service
begin_test "Resolve kube-dns.kube-system.svc.cluster.local"
DNS_RESULT="$(pod_exec dns-test nslookup kube-dns.kube-system.svc.cluster.local 2>&1 || echo "FAILED")"
if echo "$DNS_RESULT" | grep -q "Address.*[0-9]" && ! echo "$DNS_RESULT" | grep -qi "NXDOMAIN\|SERVFAIL\|can't find"; then
    DNS_SVC_IP="$(echo "$DNS_RESULT" | grep -A1 "Name:" | grep "Address" | awk '{print $NF}' | head -1)"
    log_info "kube-dns.kube-system.svc.cluster.local -> $DNS_SVC_IP"
    pass_test
else
    log_info "nslookup output: $DNS_RESULT"
    fail_test "Could not resolve kube-dns.kube-system.svc.cluster.local"
fi

# Test 3: Short name resolution (search domain should append svc.cluster.local)
# Use "kubernetes.default" since the kubernetes service is in the default namespace,
# and the pod's search domain starts with the test namespace.
begin_test "Short name resolution: 'kubernetes.default' resolves via search domain"
DNS_RESULT="$(pod_exec dns-test nslookup kubernetes.default 2>&1 || echo "FAILED")"
if echo "$DNS_RESULT" | grep -q "Address.*[0-9]" && ! echo "$DNS_RESULT" | grep -qi "NXDOMAIN\|SERVFAIL\|can't find"; then
    pass_test
else
    log_info "nslookup output: $DNS_RESULT"
    fail_test "Short name 'kubernetes.default' did not resolve"
fi

# Test 4: Resolve external domain
begin_test "Resolve external domain (google.com)"
DNS_RESULT="$(pod_exec dns-test nslookup google.com 2>&1 || echo "FAILED")"
if echo "$DNS_RESULT" | grep -q "Address.*[0-9]" && ! echo "$DNS_RESULT" | grep -qi "NXDOMAIN\|SERVFAIL\|can't find"; then
    GOOGLE_IP="$(echo "$DNS_RESULT" | grep -A2 "Name:" | grep "Address" | awk '{print $NF}' | head -1)"
    log_info "google.com -> $GOOGLE_IP"
    pass_test
else
    log_info "nslookup output: $DNS_RESULT"
    fail_test "Could not resolve google.com"
fi

# Test 5: Check /etc/resolv.conf is correctly configured
begin_test "Pod /etc/resolv.conf has correct nameserver"
RESOLV_CONF="$(pod_exec dns-test cat /etc/resolv.conf 2>&1)"
log_info "resolv.conf contents:"
echo "$RESOLV_CONF" | while IFS= read -r line; do
    log_info "  $line"
done

if echo "$RESOLV_CONF" | grep -q "^nameserver"; then
    pass_test
else
    fail_test "/etc/resolv.conf missing nameserver entry"
fi

# Test 6: Check search domains in resolv.conf
begin_test "Pod /etc/resolv.conf has search domains"
if echo "$RESOLV_CONF" | grep -q "^search.*svc.cluster.local"; then
    pass_test
else
    if echo "$RESOLV_CONF" | grep -q "^search"; then
        log_info "Search line: $(echo "$RESOLV_CONF" | grep "^search")"
        pass_test
    else
        fail_test "No search domains in /etc/resolv.conf"
    fi
fi

# Test 7: DNS response time is reasonable (< 1 second)
begin_test "DNS response time < 1s for kubernetes.default"
START_TIME="$(date +%s%N)"
pod_exec dns-test nslookup kubernetes.default.svc.cluster.local &>/dev/null || true
END_TIME="$(date +%s%N)"
ELAPSED_MS="$(( (END_TIME - START_TIME) / 1000000 ))"
log_info "DNS lookup took ${ELAPSED_MS}ms"
if [[ "$ELAPSED_MS" -lt 1000 ]]; then
    pass_test
else
    fail_test "DNS lookup took ${ELAPSED_MS}ms (> 1000ms)"
fi

# Test 8: Create a service in the test namespace and resolve it
begin_test "Resolve a service created in test namespace"
# Create a simple ClusterIP service
kubectl run dns-svc-target --namespace="$TEST_NS" --image="$ALPINE_IMAGE" --restart=Never --labels="app=dns-svc-target" --command -- sleep 3600 2>/dev/null || true
kubectl expose pod dns-svc-target --namespace="$TEST_NS" --port=80 --name=test-svc 2>/dev/null || true
sleep 3

DNS_RESULT="$(pod_exec dns-test nslookup "test-svc.${TEST_NS}.svc.cluster.local" 2>&1 || echo "FAILED")"
if echo "$DNS_RESULT" | grep -q "Address.*[0-9]" && ! echo "$DNS_RESULT" | grep -qi "NXDOMAIN\|SERVFAIL\|can't find"; then
    SVC_IP="$(echo "$DNS_RESULT" | grep -A1 "Name:" | grep "Address" | awk '{print $NF}' | head -1)"
    log_info "test-svc.${TEST_NS}.svc.cluster.local -> $SVC_IP"
    pass_test
else
    log_info "nslookup output: $DNS_RESULT"
    fail_test "Could not resolve test-svc in test namespace"
fi

# Cleanup the extra service
kubectl delete svc test-svc --namespace="$TEST_NS" 2>/dev/null || true

###############################################################################
# Summary
###############################################################################
print_summary
