#!/usr/bin/env bash
# bench-policy.sh — NetworkPolicy overhead benchmarks for NovaNet using fortio.
#
# Measures HTTP throughput and latency under increasing policy load:
#   1. 0 NetworkPolicies    (baseline)
#   2. 100 NetworkPolicies
#   3. 1000 NetworkPolicies
#
# All tests use cross-node pod placement to capture the realistic
# dataplane path. The benchmark pods themselves are not selected by
# the injected policies — this measures the overhead of the policy
# lookup / map-walk in the eBPF dataplane, not direct enforcement.
#
# Results are saved as JSON to results/policy-<timestamp>.json

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

###############################################################################
# Configuration
###############################################################################

POLICY_COUNTS=(0 100 1000)
# Fixed QPS for comparable measurements
POLICY_BENCH_QPS="${POLICY_BENCH_QPS:-1000}"
POLICY_BENCH_CONCURRENCY="${POLICY_BENCH_CONCURRENCY:-16}"

###############################################################################
# Run a policy round
###############################################################################

# run_policy_round POLICY_COUNT CLIENT_POD SERVER_IP
run_policy_round() {
    local policy_count="$1"
    local client="$2"
    local server_ip="$3"

    log_header "Policy Round: ${policy_count} NetworkPolicies"

    # Clean up previous policies.
    delete_network_policies

    # Install policies if count > 0.
    if (( policy_count > 0 )); then
        create_network_policies "$policy_count"
        log_info "Waiting 10s for policy propagation..."
        sleep 10
    fi

    local actual_count
    actual_count=$(kubectl get networkpolicy -n "$BENCHMARK_NS" --no-headers 2>/dev/null | wc -l | tr -d ' ')
    log_info "Active NetworkPolicies in namespace: $actual_count"

    # -- HTTP throughput (max QPS) --
    log_step "HTTP max QPS with ${policy_count} policies (c=${POLICY_BENCH_CONCURRENCY}, t=${DURATION})"
    local http_max_json
    http_max_json=$(run_fortio_http "$client" "$server_ip" 0 "$POLICY_BENCH_CONCURRENCY" "$DURATION")
    local http_max_summary
    http_max_summary=$(summarize_fortio_result "$http_max_json" "http-max-qps-${policy_count}pol")

    local max_qps
    max_qps=$(echo "$http_max_summary" | jq -r '.actual_qps')
    log_info "  -> Max QPS: ${max_qps}"

    # -- HTTP latency at fixed QPS --
    log_step "HTTP latency at ${POLICY_BENCH_QPS} QPS with ${policy_count} policies (t=${DURATION})"
    local http_lat_json
    http_lat_json=$(run_fortio_http "$client" "$server_ip" "$POLICY_BENCH_QPS" "$POLICY_BENCH_CONCURRENCY" "$DURATION")
    local http_lat_summary
    http_lat_summary=$(summarize_fortio_result "$http_lat_json" "http-lat-${policy_count}pol")

    local p50 p99
    p50=$(echo "$http_lat_summary" | jq -r '.latency_ms.p50')
    p99=$(echo "$http_lat_summary" | jq -r '.latency_ms.p99')
    log_info "  -> p50: ${p50}ms  p99: ${p99}ms"

    # -- TCP echo throughput --
    log_step "TCP echo max QPS with ${policy_count} policies (t=${DURATION})"
    local tcp_json
    tcp_json=$(run_fortio_tcp "$client" "$server_ip" 0 "$POLICY_BENCH_CONCURRENCY" "$DURATION")
    local tcp_summary
    tcp_summary=$(summarize_fortio_result "$tcp_json" "tcp-max-qps-${policy_count}pol")

    local tcp_qps
    tcp_qps=$(echo "$tcp_summary" | jq -r '.actual_qps')
    log_info "  -> TCP Max QPS: ${tcp_qps}"

    # Assemble round result
    jq -n \
        --argjson pc "$policy_count" \
        --argjson http_max "$http_max_summary" \
        --argjson http_lat "$http_lat_summary" \
        --argjson tcp "$tcp_summary" \
        '{
            policy_count: $pc,
            http_max_qps: $http_max,
            http_latency: $http_lat,
            tcp_max_qps: $tcp
        }'
}

###############################################################################
# Main
###############################################################################

main() {
    log_header "NovaNet Policy Overhead Benchmark (fortio)"
    check_prerequisites
    ensure_results_dir
    register_cleanup

    delete_namespace
    ensure_namespace

    # Create persistent test pods (cross-node).
    log_info "Setting up benchmark pods (cross-node)..."
    create_fortio_server "policy-server" "$WORKER_NODE_1"
    create_fortio_client "policy-client" "$WORKER_NODE_2"
    wait_pod_ready "policy-server"
    wait_pod_ready "policy-client"

    local server_ip
    server_ip=$(get_pod_ip "policy-server")

    # Run each policy-count round.
    local results=()
    for count in "${POLICY_COUNTS[@]}"; do
        local res
        res=$(run_policy_round "$count" "policy-client" "$server_ip")
        results+=("$res")
    done

    # -----------------------------------------------------------------------
    # Assemble final JSON
    # -----------------------------------------------------------------------
    local metadata
    metadata=$(collect_metadata)

    local all_results
    all_results=$(printf '%s\n' "${results[@]}" | jq -s '.')

    local final_json
    final_json=$(jq -n \
        --argjson meta "$metadata" \
        --argjson tests "$all_results" \
        '{
            benchmark: "policy-overhead",
            metadata: $meta,
            results: $tests
        }')

    local out_file="$RESULTS_DIR/policy-${TIMESTAMP}.json"
    save_json "$out_file" "$final_json"

    # -----------------------------------------------------------------------
    # Human-readable summary
    # -----------------------------------------------------------------------
    log_header "Policy Overhead Results Summary"

    echo ""
    echo "  HTTP Max QPS (higher is better):"
    local header="Policies|Max QPS|p50 (ms)|p99 (ms)|Errors"
    local rows=()
    for r in "${results[@]}"; do
        local pc qps p50 p99 errs
        pc=$(echo "$r" | jq -r '.policy_count')
        qps=$(echo "$r" | jq -r '.http_max_qps.actual_qps')
        p50=$(echo "$r" | jq -r '.http_max_qps.latency_ms.p50')
        p99=$(echo "$r" | jq -r '.http_max_qps.latency_ms.p99')
        errs=$(echo "$r" | jq -r '.http_max_qps.errors')
        rows+=("${pc}|${qps}|${p50}|${p99}|${errs}")
    done
    print_table "$header" "${rows[@]}"

    echo ""
    echo "  HTTP Latency at ${POLICY_BENCH_QPS} QPS (lower is better):"
    header="Policies|Actual QPS|p50 (ms)|p90 (ms)|p99 (ms)|p99.9 (ms)"
    rows=()
    for r in "${results[@]}"; do
        local pc aqps p50 p90 p99 p999
        pc=$(echo "$r" | jq -r '.policy_count')
        aqps=$(echo "$r" | jq -r '.http_latency.actual_qps')
        p50=$(echo "$r" | jq -r '.http_latency.latency_ms.p50')
        p90=$(echo "$r" | jq -r '.http_latency.latency_ms.p90')
        p99=$(echo "$r" | jq -r '.http_latency.latency_ms.p99')
        p999=$(echo "$r" | jq -r '.http_latency.latency_ms.p999')
        rows+=("${pc}|${aqps}|${p50}|${p90}|${p99}|${p999}")
    done
    print_table "$header" "${rows[@]}"

    echo ""
    echo "  TCP Echo Max QPS:"
    header="Policies|Max QPS|p50 (ms)|p99 (ms)"
    rows=()
    for r in "${results[@]}"; do
        local pc qps p50 p99
        pc=$(echo "$r" | jq -r '.policy_count')
        qps=$(echo "$r" | jq -r '.tcp_max_qps.actual_qps')
        p50=$(echo "$r" | jq -r '.tcp_max_qps.latency_ms.p50')
        p99=$(echo "$r" | jq -r '.tcp_max_qps.latency_ms.p99')
        rows+=("${pc}|${qps}|${p50}|${p99}")
    done
    print_table "$header" "${rows[@]}"

    # -----------------------------------------------------------------------
    # Overhead delta
    # -----------------------------------------------------------------------
    echo ""
    log_header "Policy Overhead Delta (vs. 0 policies)"

    local baseline_qps baseline_p50 baseline_p99
    baseline_qps=$(echo "${results[0]}" | jq '.http_max_qps.actual_qps')
    baseline_p50=$(echo "${results[0]}" | jq '.http_latency.latency_ms.p50')
    baseline_p99=$(echo "${results[0]}" | jq '.http_latency.latency_ms.p99')

    for (( i=1; i<${#results[@]}; i++ )); do
        local pc qps p50 p99 qps_diff qps_pct p50_diff p99_diff
        pc=$(echo "${results[$i]}" | jq '.policy_count')
        qps=$(echo "${results[$i]}" | jq '.http_max_qps.actual_qps')
        p50=$(echo "${results[$i]}" | jq '.http_latency.latency_ms.p50')
        p99=$(echo "${results[$i]}" | jq '.http_latency.latency_ms.p99')

        qps_diff=$(echo "$qps $baseline_qps" | awk '{printf "%.0f", $1 - $2}')
        qps_pct=$(echo "$qps $baseline_qps" | awk '{if ($2 > 0) printf "%.1f", (($1 - $2) / $2) * 100; else print "N/A"}')
        p50_diff=$(echo "$p50 $baseline_p50" | awk '{printf "%.3f", $1 - $2}')
        p99_diff=$(echo "$p99 $baseline_p99" | awk '{printf "%.3f", $1 - $2}')

        echo "  ${pc} policies:"
        echo "    Max QPS delta:    ${qps_diff} (${qps_pct}%)"
        echo "    p50 latency delta: +${p50_diff} ms"
        echo "    p99 latency delta: +${p99_diff} ms"
    done

    echo ""
    log_info "Full results: $out_file"
}

main "$@"
