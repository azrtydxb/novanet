#!/usr/bin/env bash
# bench-latency.sh — Latency profiling benchmarks for NovaNet using fortio.
#
# Measures latency percentiles (p50/p90/p99/p99.9) at fixed QPS rates:
#   1. Same-node HTTP + TCP echo
#   2. Cross-node HTTP + TCP echo
#   3. Host-networking baseline
#
# This test fixes QPS (not max) to measure latency under controlled load.
#
# Results are saved as JSON to results/latency-<timestamp>.json

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

###############################################################################
# Configuration
###############################################################################

QPS_RATES="${QPS_RATES:-100 500 1000 5000}"

###############################################################################
# Main
###############################################################################

main() {
    log_header "NovaNet Latency Benchmark (fortio)"
    check_prerequisites
    ensure_results_dir
    register_cleanup

    delete_namespace
    ensure_namespace

    local all_results=()

    # -----------------------------------------------------------------------
    # 1 — Same-node HTTP + TCP
    # -----------------------------------------------------------------------
    log_header "Same-Node Latency (${WORKER_NODE_1})"

    create_fortio_server "lat-same-server" "$WORKER_NODE_1"
    create_fortio_client "lat-same-client" "$WORKER_NODE_1"
    wait_pod_ready "lat-same-server"
    wait_pod_ready "lat-same-client"

    local server_ip
    server_ip=$(get_pod_ip "lat-same-server")

    for qps in $QPS_RATES; do
        local c=$((qps / 50))
        [[ $c -lt 4 ]] && c=4

        log_step "same-node-http qps=${qps} c=${c} t=${DURATION}"
        local raw_json summary
        raw_json=$(run_fortio_http "lat-same-client" "$server_ip" "$qps" "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "same-node-http-qps${qps}")
            summary=$(echo "$summary" | jq --arg proto "http" --argjson tqps "$qps" '. + {protocol: $proto, target_qps: $tqps}')
            all_results+=("$summary")
            log_info "  -> Actual QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output"
        fi
    done

    for qps in $QPS_RATES; do
        local c=$((qps / 50))
        [[ $c -lt 4 ]] && c=4

        log_step "same-node-tcp qps=${qps} c=${c} t=${DURATION}"
        local raw_json summary
        raw_json=$(run_fortio_tcp "lat-same-client" "$server_ip" "$qps" "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "same-node-tcp-qps${qps}")
            summary=$(echo "$summary" | jq --arg proto "tcp" --argjson tqps "$qps" '. + {protocol: $proto, target_qps: $tqps}')
            all_results+=("$summary")
            log_info "  -> Actual QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output"
        fi
    done

    delete_pod "lat-same-server"
    delete_pod "lat-same-client"

    # -----------------------------------------------------------------------
    # 2 — Cross-node HTTP + TCP
    # -----------------------------------------------------------------------
    log_header "Cross-Node Latency (${WORKER_NODE_1} <-> ${WORKER_NODE_2})"

    create_fortio_server "lat-cross-server" "$WORKER_NODE_1"
    create_fortio_client "lat-cross-client" "$WORKER_NODE_2"
    wait_pod_ready "lat-cross-server"
    wait_pod_ready "lat-cross-client"

    server_ip=$(get_pod_ip "lat-cross-server")

    for qps in $QPS_RATES; do
        local c=$((qps / 50))
        [[ $c -lt 4 ]] && c=4

        log_step "cross-node-http qps=${qps} c=${c} t=${DURATION}"
        local raw_json summary
        raw_json=$(run_fortio_http "lat-cross-client" "$server_ip" "$qps" "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "cross-node-http-qps${qps}")
            summary=$(echo "$summary" | jq --arg proto "http" --argjson tqps "$qps" '. + {protocol: $proto, target_qps: $tqps}')
            all_results+=("$summary")
            log_info "  -> Actual QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output"
        fi
    done

    for qps in $QPS_RATES; do
        local c=$((qps / 50))
        [[ $c -lt 4 ]] && c=4

        log_step "cross-node-tcp qps=${qps} c=${c} t=${DURATION}"
        local raw_json summary
        raw_json=$(run_fortio_tcp "lat-cross-client" "$server_ip" "$qps" "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "cross-node-tcp-qps${qps}")
            summary=$(echo "$summary" | jq --arg proto "tcp" --argjson tqps "$qps" '. + {protocol: $proto, target_qps: $tqps}')
            all_results+=("$summary")
            log_info "  -> Actual QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output"
        fi
    done

    delete_pod "lat-cross-server"
    delete_pod "lat-cross-client"

    # -----------------------------------------------------------------------
    # 3 — Host-networking baseline
    # -----------------------------------------------------------------------
    log_header "Host-Networking Baseline (${WORKER_NODE_1} <-> ${WORKER_NODE_2})"

    create_fortio_server "lat-host-server" "$WORKER_NODE_1" --host-network
    create_fortio_client "lat-host-client" "$WORKER_NODE_2" --host-network
    wait_pod_ready "lat-host-server"
    wait_pod_ready "lat-host-client"

    server_ip=$(kubectl get node "$WORKER_NODE_1" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')

    for qps in $QPS_RATES; do
        local c=$((qps / 50))
        [[ $c -lt 4 ]] && c=4

        log_step "host-net-http qps=${qps} c=${c} t=${DURATION}"
        local raw_json summary
        raw_json=$(run_fortio_http "lat-host-client" "$server_ip" "$qps" "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "host-net-http-qps${qps}")
            summary=$(echo "$summary" | jq --arg proto "http" --argjson tqps "$qps" '. + {protocol: $proto, target_qps: $tqps}')
            all_results+=("$summary")
            log_info "  -> Actual QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output"
        fi
    done

    delete_pod "lat-host-server"
    delete_pod "lat-host-client"

    # -----------------------------------------------------------------------
    # Assemble JSON & summary
    # -----------------------------------------------------------------------
    if [[ ${#all_results[@]} -eq 0 ]]; then
        log_error "No benchmark results collected!"
        return 1
    fi

    local metadata
    metadata=$(collect_metadata)

    local json_results
    json_results=$(printf '%s\n' "${all_results[@]}" | jq -s '.')

    local final_json
    final_json=$(jq -n \
        --argjson meta "$metadata" \
        --argjson tests "$json_results" \
        '{
            benchmark: "latency",
            metadata: $meta,
            results: $tests
        }')

    local out_file="$RESULTS_DIR/latency-${TIMESTAMP}.json"
    save_json "$out_file" "$final_json"

    log_header "Latency Results Summary"

    local header="Test|Proto|Target QPS|Actual QPS|p50 (ms)|p90 (ms)|p99 (ms)|p99.9 (ms)|Errors"
    local rows=()
    for r in "${all_results[@]}"; do
        local name proto tqps aqps p50 p90 p99 p999 errs
        name=$(echo "$r" | jq -r '.test')
        proto=$(echo "$r" | jq -r '.protocol | ascii_upcase')
        tqps=$(echo "$r" | jq -r '.target_qps')
        aqps=$(echo "$r" | jq -r '.actual_qps')
        p50=$(echo "$r" | jq -r '.latency_ms.p50')
        p90=$(echo "$r" | jq -r '.latency_ms.p90')
        p99=$(echo "$r" | jq -r '.latency_ms.p99')
        p999=$(echo "$r" | jq -r '.latency_ms.p999')
        errs=$(echo "$r" | jq -r '.errors')
        rows+=("${name}|${proto}|${tqps}|${aqps}|${p50}|${p90}|${p99}|${p999}|${errs}")
    done

    print_table "$header" "${rows[@]}"

    # CNI overhead estimate
    echo ""
    log_header "CNI Overhead Estimate (cross-node HTTP vs host-network HTTP)"

    for qps in $QPS_RATES; do
        local cross_p50 host_p50 cross_p99 host_p99
        cross_p50=$(printf '%s\n' "${all_results[@]}" | jq -r "select(.test == \"cross-node-http-qps${qps}\") | .latency_ms.p50" 2>/dev/null | head -1)
        host_p50=$(printf '%s\n' "${all_results[@]}" | jq -r "select(.test == \"host-net-http-qps${qps}\") | .latency_ms.p50" 2>/dev/null | head -1)
        cross_p99=$(printf '%s\n' "${all_results[@]}" | jq -r "select(.test == \"cross-node-http-qps${qps}\") | .latency_ms.p99" 2>/dev/null | head -1)
        host_p99=$(printf '%s\n' "${all_results[@]}" | jq -r "select(.test == \"host-net-http-qps${qps}\") | .latency_ms.p99" 2>/dev/null | head -1)

        if [[ -n "$cross_p50" && -n "$host_p50" && "$cross_p50" != "null" && "$host_p50" != "null" ]]; then
            local delta_p50 delta_p99
            delta_p50=$(echo "$cross_p50 $host_p50" | awk '{printf "%.3f", $1 - $2}')
            delta_p99=$(echo "$cross_p99 $host_p99" | awk '{printf "%.3f", $1 - $2}')
            echo "  QPS ${qps}:  p50 overhead: +${delta_p50}ms   p99 overhead: +${delta_p99}ms"
        fi
    done

    echo ""
    log_info "Full results: $out_file"
}

main "$@"
