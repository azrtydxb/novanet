#!/usr/bin/env bash
# bench-bandwidth.sh — Network bandwidth benchmarks for NovaNet using iperf3.
#
# Measures actual throughput (Gbps/Mbps) using iperf3:
#   1. Same-node TCP (upload + download, 1 & 4 streams)
#   2. Cross-node TCP (upload + download, 1 & 4 streams)
#   3. Cross-node UDP at increasing target bitrates
#   4. Host-networking TCP baseline
#
# This complements the fortio QPS benchmarks by measuring raw network
# bandwidth capacity rather than request rates.
#
# Results are saved as JSON to results/bandwidth-<timestamp>.json

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

###############################################################################
# Configuration
###############################################################################

IPERF_DURATION="${IPERF_DURATION:-10}"
PARALLEL_STREAMS="${PARALLEL_STREAMS:-1 4}"
UDP_BITRATES="${UDP_BITRATES:-100M 500M 1G 0}"  # 0 = unlimited

###############################################################################
# Main
###############################################################################

main() {
    log_header "NovaNet Bandwidth Benchmark (iperf3)"
    check_prerequisites
    ensure_results_dir
    register_cleanup

    delete_namespace
    ensure_namespace

    local all_results=()

    # -------------------------------------------------------------------
    # 1 — Same-node TCP bandwidth
    # -------------------------------------------------------------------
    log_header "Same-Node TCP Bandwidth (${WORKER_NODE_1})"

    create_iperf3_server "bw-same-server" "$WORKER_NODE_1"
    create_iperf3_client "bw-same-client" "$WORKER_NODE_1"
    wait_pod_ready "bw-same-server"
    wait_pod_ready "bw-same-client"

    local server_ip
    server_ip=$(get_pod_ip "bw-same-server")

    for streams in $PARALLEL_STREAMS; do
        # Upload (client -> server)
        log_step "same-node-tcp-upload P=${streams} t=${IPERF_DURATION}s"
        local raw_json summary
        raw_json=$(run_iperf3_tcp "bw-same-client" "$server_ip" "$IPERF_DURATION" "$streams") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq -e '.end' &>/dev/null; then
            summary=$(summarize_iperf3_result "$raw_json" "same-node-tcp-upload-P${streams}")
            summary=$(echo "$summary" | jq --arg dir "upload" --argjson p "$streams" '. + {direction: $dir, parallel_streams: $p}')
            all_results+=("$summary")
            log_info "  -> Send: $(echo "$summary" | jq -r '.sent.bandwidth_gbps') Gbps  Recv: $(echo "$summary" | jq -r '.received.bandwidth_gbps') Gbps  Retransmits: $(echo "$summary" | jq -r '.sent.retransmits')"
        else
            log_warn "  -> No valid output"
        fi

        # Download (server -> client, reverse mode)
        log_step "same-node-tcp-download P=${streams} t=${IPERF_DURATION}s"
        raw_json=$(run_iperf3_tcp_reverse "bw-same-client" "$server_ip" "$IPERF_DURATION" "$streams") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq -e '.end' &>/dev/null; then
            summary=$(summarize_iperf3_result "$raw_json" "same-node-tcp-download-P${streams}")
            summary=$(echo "$summary" | jq --arg dir "download" --argjson p "$streams" '. + {direction: $dir, parallel_streams: $p}')
            all_results+=("$summary")
            log_info "  -> Send: $(echo "$summary" | jq -r '.sent.bandwidth_gbps') Gbps  Recv: $(echo "$summary" | jq -r '.received.bandwidth_gbps') Gbps"
        else
            log_warn "  -> No valid output"
        fi
    done

    delete_pod "bw-same-server"
    delete_pod "bw-same-client"

    # -------------------------------------------------------------------
    # 2 — Cross-node TCP bandwidth
    # -------------------------------------------------------------------
    log_header "Cross-Node TCP Bandwidth (${WORKER_NODE_1} <-> ${WORKER_NODE_2})"

    create_iperf3_server "bw-cross-server" "$WORKER_NODE_1"
    create_iperf3_client "bw-cross-client" "$WORKER_NODE_2"
    wait_pod_ready "bw-cross-server"
    wait_pod_ready "bw-cross-client"

    server_ip=$(get_pod_ip "bw-cross-server")

    for streams in $PARALLEL_STREAMS; do
        # Upload
        log_step "cross-node-tcp-upload P=${streams} t=${IPERF_DURATION}s"
        local raw_json summary
        raw_json=$(run_iperf3_tcp "bw-cross-client" "$server_ip" "$IPERF_DURATION" "$streams") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq -e '.end' &>/dev/null; then
            summary=$(summarize_iperf3_result "$raw_json" "cross-node-tcp-upload-P${streams}")
            summary=$(echo "$summary" | jq --arg dir "upload" --argjson p "$streams" '. + {direction: $dir, parallel_streams: $p}')
            all_results+=("$summary")
            log_info "  -> Send: $(echo "$summary" | jq -r '.sent.bandwidth_gbps') Gbps  Recv: $(echo "$summary" | jq -r '.received.bandwidth_gbps') Gbps  Retransmits: $(echo "$summary" | jq -r '.sent.retransmits')"
        else
            log_warn "  -> No valid output"
        fi

        # Download
        log_step "cross-node-tcp-download P=${streams} t=${IPERF_DURATION}s"
        raw_json=$(run_iperf3_tcp_reverse "bw-cross-client" "$server_ip" "$IPERF_DURATION" "$streams") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq -e '.end' &>/dev/null; then
            summary=$(summarize_iperf3_result "$raw_json" "cross-node-tcp-download-P${streams}")
            summary=$(echo "$summary" | jq --arg dir "download" --argjson p "$streams" '. + {direction: $dir, parallel_streams: $p}')
            all_results+=("$summary")
            log_info "  -> Send: $(echo "$summary" | jq -r '.sent.bandwidth_gbps') Gbps  Recv: $(echo "$summary" | jq -r '.received.bandwidth_gbps') Gbps"
        else
            log_warn "  -> No valid output"
        fi
    done

    # -------------------------------------------------------------------
    # 3 — Cross-node UDP bandwidth
    # -------------------------------------------------------------------
    log_header "Cross-Node UDP Bandwidth (${WORKER_NODE_1} <-> ${WORKER_NODE_2})"

    # iperf3 only allows one client at a time, server is still running
    for bitrate in $UDP_BITRATES; do
        local label="${bitrate}"
        [[ "$bitrate" == "0" ]] && label="unlimited"

        log_step "cross-node-udp bitrate=${label} t=${IPERF_DURATION}s"
        local raw_json summary
        raw_json=$(run_iperf3_udp "bw-cross-client" "$server_ip" "$IPERF_DURATION" "$bitrate") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq -e '.end' &>/dev/null; then
            summary=$(summarize_iperf3_result "$raw_json" "cross-node-udp-${label}")
            summary=$(echo "$summary" | jq --arg br "$label" '. + {target_bitrate: $br, protocol: "udp"}')
            all_results+=("$summary")
            log_info "  -> Bandwidth: $(echo "$summary" | jq -r '.sent.bandwidth_mbps') Mbps  Jitter: $(echo "$summary" | jq -r '.sent.jitter_ms')ms  Loss: $(echo "$summary" | jq -r '.sent.lost_percent')%"
        else
            log_warn "  -> No valid output"
        fi
    done

    delete_pod "bw-cross-server"
    delete_pod "bw-cross-client"

    # -------------------------------------------------------------------
    # 4 — Host-networking TCP baseline
    # -------------------------------------------------------------------
    log_header "Host-Networking TCP Baseline (${WORKER_NODE_1} <-> ${WORKER_NODE_2})"

    create_iperf3_server "bw-host-server" "$WORKER_NODE_1" --host-network
    create_iperf3_client "bw-host-client" "$WORKER_NODE_2" --host-network
    wait_pod_ready "bw-host-server"
    wait_pod_ready "bw-host-client"

    server_ip=$(kubectl get node "$WORKER_NODE_1" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')

    for streams in $PARALLEL_STREAMS; do
        log_step "host-net-tcp-upload P=${streams} t=${IPERF_DURATION}s"
        local raw_json summary
        raw_json=$(run_iperf3_tcp "bw-host-client" "$server_ip" "$IPERF_DURATION" "$streams") || true
        if [[ -n "$raw_json" ]] && echo "$raw_json" | jq -e '.end' &>/dev/null; then
            summary=$(summarize_iperf3_result "$raw_json" "host-net-tcp-upload-P${streams}")
            summary=$(echo "$summary" | jq --arg dir "upload" --argjson p "$streams" '. + {direction: $dir, parallel_streams: $p}')
            all_results+=("$summary")
            log_info "  -> Send: $(echo "$summary" | jq -r '.sent.bandwidth_gbps') Gbps  Recv: $(echo "$summary" | jq -r '.received.bandwidth_gbps') Gbps  Retransmits: $(echo "$summary" | jq -r '.sent.retransmits')"
        else
            log_warn "  -> No valid output"
        fi
    done

    delete_pod "bw-host-server"
    delete_pod "bw-host-client"

    # -------------------------------------------------------------------
    # Assemble JSON & summary
    # -------------------------------------------------------------------
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
        --arg iperf_dur "$IPERF_DURATION" \
        '{
            benchmark: "bandwidth",
            metadata: ($meta + {iperf_duration_secs: ($iperf_dur | tonumber)}),
            results: $tests
        }')

    local out_file="$RESULTS_DIR/bandwidth-${TIMESTAMP}.json"
    save_json "$out_file" "$final_json"

    # -------------------------------------------------------------------
    # Human-readable summary
    # -------------------------------------------------------------------
    log_header "TCP Bandwidth Results (Gbps)"

    local header="Test|Dir|Streams|Send (Gbps)|Recv (Gbps)|Retransmits|CPU Host|CPU Remote"
    local rows=()
    for r in "${all_results[@]}"; do
        # Skip UDP entries
        local proto
        proto=$(echo "$r" | jq -r '.protocol // "tcp"')
        [[ "$proto" == "udp" ]] && continue

        local name dir streams send recv retrans cpu_h cpu_r
        name=$(echo "$r" | jq -r '.test')
        dir=$(echo "$r" | jq -r '.direction // "upload"')
        streams=$(echo "$r" | jq -r '.parallel_streams // 1')
        send=$(echo "$r" | jq -r '.sent.bandwidth_gbps')
        recv=$(echo "$r" | jq -r '.received.bandwidth_gbps // "N/A"')
        retrans=$(echo "$r" | jq -r '.sent.retransmits // 0')
        cpu_h=$(echo "$r" | jq -r '.cpu_utilization.host_total | (. * 10 | round / 10) | tostring + "%"')
        cpu_r=$(echo "$r" | jq -r '.cpu_utilization.remote_total | (. * 10 | round / 10) | tostring + "%"')
        rows+=("${name}|${dir}|${streams}|${send}|${recv}|${retrans}|${cpu_h}|${cpu_r}")
    done

    if [[ ${#rows[@]} -gt 0 ]]; then
        print_table "$header" "${rows[@]}"
    fi

    # UDP summary
    local udp_rows=()
    for r in "${all_results[@]}"; do
        local proto
        proto=$(echo "$r" | jq -r '.protocol // "tcp"')
        [[ "$proto" != "udp" ]] && continue

        local name bitrate bw jitter loss_pct
        name=$(echo "$r" | jq -r '.test')
        bitrate=$(echo "$r" | jq -r '.target_bitrate')
        bw=$(echo "$r" | jq -r '.sent.bandwidth_mbps')
        jitter=$(echo "$r" | jq -r '.sent.jitter_ms // 0')
        loss_pct=$(echo "$r" | jq -r '.sent.lost_percent // 0')
        udp_rows+=("${name}|${bitrate}|${bw}|${jitter}|${loss_pct}")
    done

    if [[ ${#udp_rows[@]} -gt 0 ]]; then
        echo ""
        log_header "UDP Bandwidth Results"
        local udp_header="Test|Target|Actual (Mbps)|Jitter (ms)|Loss (%)"
        print_table "$udp_header" "${udp_rows[@]}"
    fi

    # -------------------------------------------------------------------
    # CNI overhead estimate
    # -------------------------------------------------------------------
    echo ""
    log_header "CNI Bandwidth Overhead (cross-node vs host-network)"

    for streams in $PARALLEL_STREAMS; do
        local cross_bw host_bw
        cross_bw=$(printf '%s\n' "${all_results[@]}" | jq -r "select(.test == \"cross-node-tcp-upload-P${streams}\") | .received.bandwidth_gbps" 2>/dev/null | head -1)
        host_bw=$(printf '%s\n' "${all_results[@]}" | jq -r "select(.test == \"host-net-tcp-upload-P${streams}\") | .received.bandwidth_gbps" 2>/dev/null | head -1)

        if [[ -n "$cross_bw" && -n "$host_bw" && "$cross_bw" != "null" && "$host_bw" != "null" ]]; then
            local delta pct
            delta=$(echo "$cross_bw $host_bw" | awk '{printf "%.3f", $1 - $2}')
            pct=$(echo "$cross_bw $host_bw" | awk '{if ($2 > 0) printf "%.1f", (($1 - $2) / $2) * 100; else print "N/A"}')
            echo "  P=${streams}:  CNI: ${cross_bw} Gbps  Host: ${host_bw} Gbps  Delta: ${delta} Gbps (${pct}%)"
        fi
    done

    echo ""
    log_info "Full results: $out_file"
}

main "$@"
