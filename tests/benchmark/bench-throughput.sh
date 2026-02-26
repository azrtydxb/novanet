#!/usr/bin/env bash
# bench-throughput.sh — HTTP & TCP throughput benchmarks for NovaNet using fortio.
#
# Measures maximum QPS at increasing concurrency levels for:
#   1. Same-node (HTTP + TCP echo)
#   2. Cross-node (HTTP + TCP echo)
#   3. Host-networking baseline (HTTP)
#
# Results are saved as JSON to results/throughput-<timestamp>.json

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

###############################################################################
# Configuration
###############################################################################

CONCURRENCIES="${CONCURRENCIES:-1 4 16 64}"

###############################################################################
# Main
###############################################################################

main() {
    log_header "NovaNet Throughput Benchmark (fortio)"
    check_prerequisites
    ensure_results_dir
    register_cleanup

    delete_namespace
    ensure_namespace

    local all_results=()

    # -----------------------------------------------------------------------
    # 1 — Same-node HTTP + TCP
    # -----------------------------------------------------------------------
    log_header "Same-Node Tests (${WORKER_NODE_1})"

    create_fortio_server "same-server" "$WORKER_NODE_1"
    create_fortio_client "same-client" "$WORKER_NODE_1"
    wait_pod_ready "same-server"
    wait_pod_ready "same-client"

    local server_ip
    server_ip=$(get_pod_ip "same-server")

    for c in $CONCURRENCIES; do
        log_step "same-node-http c=${c} (max QPS, t=${DURATION})"
        local raw_json summary
        raw_json=$(run_fortio_http "same-client" "$server_ip" 0 "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "same-node-http-c${c}")
            summary=$(echo "$summary" | jq --arg proto "http" --argjson c "$c" '. + {protocol: $proto, concurrency: $c}')
            all_results+=("$summary")
            log_info "  -> QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output for same-node-http c=${c}"
        fi
    done

    for c in $CONCURRENCIES; do
        log_step "same-node-tcp c=${c} (max QPS, t=${DURATION})"
        local raw_json summary
        raw_json=$(run_fortio_tcp "same-client" "$server_ip" 0 "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "same-node-tcp-c${c}")
            summary=$(echo "$summary" | jq --arg proto "tcp" --argjson c "$c" '. + {protocol: $proto, concurrency: $c}')
            all_results+=("$summary")
            log_info "  -> QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output for same-node-tcp c=${c}"
        fi
    done

    delete_pod "same-server"
    delete_pod "same-client"

    # -----------------------------------------------------------------------
    # 2 — Cross-node HTTP + TCP
    # -----------------------------------------------------------------------
    log_header "Cross-Node Tests (${WORKER_NODE_1} <-> ${WORKER_NODE_2})"

    create_fortio_server "cross-server" "$WORKER_NODE_1"
    create_fortio_client "cross-client" "$WORKER_NODE_2"
    wait_pod_ready "cross-server"
    wait_pod_ready "cross-client"

    server_ip=$(get_pod_ip "cross-server")

    for c in $CONCURRENCIES; do
        log_step "cross-node-http c=${c} (max QPS, t=${DURATION})"
        local raw_json summary
        raw_json=$(run_fortio_http "cross-client" "$server_ip" 0 "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "cross-node-http-c${c}")
            summary=$(echo "$summary" | jq --arg proto "http" --argjson c "$c" '. + {protocol: $proto, concurrency: $c}')
            all_results+=("$summary")
            log_info "  -> QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output for cross-node-http c=${c}"
        fi
    done

    for c in $CONCURRENCIES; do
        log_step "cross-node-tcp c=${c} (max QPS, t=${DURATION})"
        local raw_json summary
        raw_json=$(run_fortio_tcp "cross-client" "$server_ip" 0 "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "cross-node-tcp-c${c}")
            summary=$(echo "$summary" | jq --arg proto "tcp" --argjson c "$c" '. + {protocol: $proto, concurrency: $c}')
            all_results+=("$summary")
            log_info "  -> QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output for cross-node-tcp c=${c}"
        fi
    done

    delete_pod "cross-server"
    delete_pod "cross-client"

    # -----------------------------------------------------------------------
    # 3 — Host-networking HTTP baseline
    # -----------------------------------------------------------------------
    log_header "Host-Networking Baseline (${WORKER_NODE_1} <-> ${WORKER_NODE_2})"

    create_fortio_server "host-server" "$WORKER_NODE_1" --host-network
    create_fortio_client "host-client" "$WORKER_NODE_2" --host-network
    wait_pod_ready "host-server"
    wait_pod_ready "host-client"

    server_ip=$(kubectl get node "$WORKER_NODE_1" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')

    for c in $CONCURRENCIES; do
        log_step "host-net-http c=${c} (max QPS, t=${DURATION})"
        local raw_json summary
        raw_json=$(run_fortio_http "host-client" "$server_ip" 0 "$c" "$DURATION") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq empty 2>/dev/null; then
            summary=$(summarize_fortio_result "$raw_json" "host-net-http-c${c}")
            summary=$(echo "$summary" | jq --arg proto "http" --argjson c "$c" '. + {protocol: $proto, concurrency: $c}')
            all_results+=("$summary")
            log_info "  -> QPS: $(echo "$summary" | jq -r '.actual_qps')  p50: $(echo "$summary" | jq -r '.latency_ms.p50')ms  p99: $(echo "$summary" | jq -r '.latency_ms.p99')ms"
        else
            log_warn "  -> No valid output for host-net-http c=${c}"
        fi
    done

    delete_pod "host-server"
    delete_pod "host-client"

    # -----------------------------------------------------------------------
    # Assemble final JSON & summary
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
            benchmark: "throughput",
            metadata: $meta,
            results: $tests
        }')

    local out_file="$RESULTS_DIR/throughput-${TIMESTAMP}.json"
    save_json "$out_file" "$final_json"

    log_header "Throughput Results Summary"

    local header="Test|Proto|Conc|QPS|p50 (ms)|p90 (ms)|p99 (ms)|Errors"
    local rows=()
    for r in "${all_results[@]}"; do
        local name proto c qps p50 p90 p99 errs
        name=$(echo "$r" | jq -r '.test')
        proto=$(echo "$r" | jq -r '.protocol | ascii_upcase')
        c=$(echo "$r" | jq -r '.concurrency')
        qps=$(echo "$r" | jq -r '.actual_qps')
        p50=$(echo "$r" | jq -r '.latency_ms.p50')
        p90=$(echo "$r" | jq -r '.latency_ms.p90')
        p99=$(echo "$r" | jq -r '.latency_ms.p99')
        errs=$(echo "$r" | jq -r '.errors')
        rows+=("${name}|${proto}|${c}|${qps}|${p50}|${p90}|${p99}|${errs}")
    done

    print_table "$header" "${rows[@]}"
    echo ""
    log_info "Full results: $out_file"
}

main "$@"
