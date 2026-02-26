#!/usr/bin/env bash
# 08-external.sh — Test external connectivity from pods.
#
# Verifies:
#   1. Pod can HTTP GET an external service
#   2. Pod can reach HTTPS endpoints
#   3. Pod can download content (validates full TCP path)
#   4. Various external endpoints reachable
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "Test 08: External Connectivity"

###############################################################################
# Setup
###############################################################################
ensure_test_ns
register_cleanup

create_pod "external-test" "" "app=novanet-test,role=external"
wait_pod_ready "external-test"

POD_IP="$(get_pod_ip external-test)"
log_info "External test pod: $POD_IP"

###############################################################################
# Tests
###############################################################################

# Test 1: HTTP GET to httpbin.org
begin_test "HTTP GET httpbin.org/get"
HTTP_STATUS="$(pod_exec external-test curl -s -o /dev/null -w '%{http_code}' --connect-timeout 10 --max-time 20 http://httpbin.org/get 2>/dev/null || echo "000")"
if [[ "$HTTP_STATUS" == "200" ]]; then
    pass_test
else
    log_info "HTTP status: $HTTP_STATUS"
    fail_test "httpbin.org returned $HTTP_STATUS (expected 200)"
fi

# Test 2: HTTPS GET (try multiple endpoints for resilience)
begin_test "HTTPS GET to external endpoint"
HTTP_STATUS="$(pod_exec external-test curl -s -o /dev/null -w '%{http_code}' --connect-timeout 10 --max-time 20 https://www.google.com 2>/dev/null || echo "000")"
if [[ "$HTTP_STATUS" =~ ^(200|301|302)$ ]]; then
    log_info "HTTPS GET google.com returned $HTTP_STATUS"
    pass_test
else
    # Fallback to example.com
    HTTP_STATUS="$(pod_exec external-test curl -s -o /dev/null -w '%{http_code}' --connect-timeout 10 --max-time 20 https://example.com 2>/dev/null || echo "000")"
    if [[ "$HTTP_STATUS" == "200" ]]; then
        log_info "HTTPS GET example.com returned $HTTP_STATUS"
        pass_test
    else
        log_info "HTTPS status: $HTTP_STATUS"
        fail_test "HTTPS GET failed (google.com and example.com)"
    fi
fi

# Test 3: HTTPS with TLS verification (validates CA bundle)
begin_test "HTTPS with TLS verification (google.com)"
TLS_RESULT="$(pod_exec external-test curl -sS --connect-timeout 10 --max-time 20 -o /dev/null -w '%{http_code}:%{ssl_verify_result}' https://www.google.com 2>/dev/null || echo "000:99")"
HTTP_CODE="$(echo "$TLS_RESULT" | cut -d: -f1)"
SSL_VERIFY="$(echo "$TLS_RESULT" | cut -d: -f2)"

if [[ "$HTTP_CODE" =~ ^(200|301|302)$ ]]; then
    if [[ "$SSL_VERIFY" == "0" ]]; then
        log_info "TLS verified successfully (ssl_verify_result=0)"
    else
        log_info "TLS verify result: $SSL_VERIFY (non-zero but connection succeeded)"
    fi
    pass_test
else
    fail_test "HTTPS to google.com returned $HTTP_CODE (ssl_verify=$SSL_VERIFY)"
fi

# Test 4: Download content and verify body (validates full TCP data path)
begin_test "Download and verify content from httpbin.org"
RESPONSE_BODY="$(pod_exec external-test curl -s --connect-timeout 10 --max-time 20 http://httpbin.org/user-agent 2>/dev/null || echo "")"
if echo "$RESPONSE_BODY" | grep -q '"user-agent"'; then
    log_info "Response body received and valid"
    pass_test
else
    if [[ -z "$RESPONSE_BODY" ]]; then
        fail_test "Empty response body"
    else
        log_info "Response: $(echo "$RESPONSE_BODY" | head -3)"
        fail_test "Unexpected response body"
    fi
fi

# Test 5: TCP connect to a well-known port (DNS over TCP)
begin_test "TCP connect to 8.8.8.8:53 (DNS over TCP)"
if pod_exec external-test bash -c "echo | nc -w 5 8.8.8.8 53" &>/dev/null; then
    pass_test
else
    fail_test "TCP connect to 8.8.8.8:53 failed"
fi

# Test 6: TCP connect to an HTTPS port
begin_test "TCP connect to 1.1.1.1:443 (Cloudflare HTTPS)"
if pod_exec external-test bash -c "echo | nc -w 5 1.1.1.1 443" &>/dev/null; then
    pass_test
else
    fail_test "TCP connect to 1.1.1.1:443 failed"
fi

# Test 7: Verify that the external IP seen is the node's IP (not the pod IP)
begin_test "External request uses node IP (SNAT/masquerade)"
SEEN_IP="$(pod_exec external-test curl -s --connect-timeout 10 --max-time 15 http://ifconfig.me 2>/dev/null || echo "")"
if [[ -z "$SEEN_IP" ]]; then
    SEEN_IP="$(pod_exec external-test curl -s --connect-timeout 10 --max-time 15 http://api.ipify.org 2>/dev/null || echo "")"
fi

if [[ -n "$SEEN_IP" ]]; then
    if [[ "$SEEN_IP" == "$POD_IP" ]]; then
        fail_test "External IP matches pod IP ($POD_IP) — SNAT not working"
    else
        log_info "External IP: $SEEN_IP (pod IP: $POD_IP) — SNAT working"
        pass_test
    fi
else
    skip_test "Could not determine external-facing IP"
fi

# Test 8: Large payload download (verify MTU / fragmentation handling)
begin_test "Download larger payload (MTU / fragmentation test)"
DOWNLOAD_SIZE="$(pod_exec external-test curl -s --connect-timeout 15 --max-time 30 -o /dev/null -w '%{size_download}' http://httpbin.org/bytes/65536 2>/dev/null || echo "0")"
if [[ "$DOWNLOAD_SIZE" -ge 60000 ]]; then
    log_info "Downloaded ${DOWNLOAD_SIZE} bytes (expected ~65536)"
    pass_test
elif [[ "$DOWNLOAD_SIZE" -gt 0 ]]; then
    log_info "Downloaded ${DOWNLOAD_SIZE} bytes (less than expected 65536)"
    fail_test "Partial download — possible MTU/fragmentation issue"
else
    fail_test "Download failed (0 bytes received)"
fi

###############################################################################
# Summary
###############################################################################
print_summary
