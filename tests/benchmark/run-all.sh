#!/usr/bin/env bash
# run-all.sh — Run all NovaNet benchmarks and produce a combined summary.
#
# Usage:
#   ./run-all.sh              # Run all benchmarks
#   ./run-all.sh throughput   # Run only throughput
#   ./run-all.sh latency      # Run only latency
#   ./run-all.sh bandwidth    # Run only bandwidth
#   ./run-all.sh policy       # Run only policy overhead
#
# Environment variables (all optional):
#   KUBECONFIG          — Path to kubeconfig (default: /etc/rancher/k3s/k3s.yaml)
#   WORKER_NODE_1       — First worker node  (default: worker-21)
#   WORKER_NODE_2       — Second worker node (default: worker-22)
#   BENCHMARK_NS        — Kubernetes namespace (default: novanet-bench)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

###############################################################################
# Which benchmarks to run
###############################################################################

REQUESTED="${1:-all}"

run_throughput=false
run_latency=false
run_bandwidth=false
run_policy=false

case "$REQUESTED" in
    all)
        run_throughput=true
        run_latency=true
        run_bandwidth=true
        run_policy=true
        ;;
    throughput)
        run_throughput=true
        ;;
    latency)
        run_latency=true
        ;;
    bandwidth)
        run_bandwidth=true
        ;;
    policy)
        run_policy=true
        ;;
    *)
        log_error "Unknown benchmark: $REQUESTED"
        echo "Usage: $0 [all|throughput|latency|bandwidth|policy]"
        exit 1
        ;;
esac

###############################################################################
# Main
###############################################################################

main() {
    log_header "NovaNet Benchmark Suite"
    echo "  Timestamp:    $TIMESTAMP"
    echo "  Cluster:      $(kubectl cluster-info 2>/dev/null | head -1 || echo 'unknown')"
    echo "  Worker Node 1: $WORKER_NODE_1"
    echo "  Worker Node 2: $WORKER_NODE_2"
    echo "  Results dir:  $RESULTS_DIR"
    echo ""

    check_prerequisites
    ensure_results_dir

    local start_time
    start_time=$(date +%s)
    local suite_ok=true
    local ran=0 passed=0 failed=0

    # -----------------------------------------------------------------------
    # Throughput
    # -----------------------------------------------------------------------
    if $run_throughput; then
        log_header "Running: Throughput Benchmark"
        ran=$((ran + 1))
        if bash "$SCRIPT_DIR/bench-throughput.sh"; then
            passed=$((passed + 1))
            log_info "Throughput benchmark completed successfully."
        else
            failed=$((failed + 1))
            suite_ok=false
            log_error "Throughput benchmark FAILED."
        fi
        echo ""
    fi

    # -----------------------------------------------------------------------
    # Latency
    # -----------------------------------------------------------------------
    if $run_latency; then
        log_header "Running: Latency Benchmark"
        ran=$((ran + 1))
        if bash "$SCRIPT_DIR/bench-latency.sh"; then
            passed=$((passed + 1))
            log_info "Latency benchmark completed successfully."
        else
            failed=$((failed + 1))
            suite_ok=false
            log_error "Latency benchmark FAILED."
        fi
        echo ""
    fi

    # -----------------------------------------------------------------------
    # Bandwidth
    # -----------------------------------------------------------------------
    if $run_bandwidth; then
        log_header "Running: Bandwidth Benchmark"
        ran=$((ran + 1))
        if bash "$SCRIPT_DIR/bench-bandwidth.sh"; then
            passed=$((passed + 1))
            log_info "Bandwidth benchmark completed successfully."
        else
            failed=$((failed + 1))
            suite_ok=false
            log_error "Bandwidth benchmark FAILED."
        fi
        echo ""
    fi

    # -----------------------------------------------------------------------
    # Policy overhead
    # -----------------------------------------------------------------------
    if $run_policy; then
        log_header "Running: Policy Overhead Benchmark"
        ran=$((ran + 1))
        if bash "$SCRIPT_DIR/bench-policy.sh"; then
            passed=$((passed + 1))
            log_info "Policy overhead benchmark completed successfully."
        else
            failed=$((failed + 1))
            suite_ok=false
            log_error "Policy overhead benchmark FAILED."
        fi
        echo ""
    fi

    # -----------------------------------------------------------------------
    # Summary
    # -----------------------------------------------------------------------
    local end_time elapsed
    end_time=$(date +%s)
    elapsed=$((end_time - start_time))

    log_header "Benchmark Suite Summary"
    echo "  Ran:      $ran benchmarks"
    echo "  Passed:   $passed"
    echo "  Failed:   $failed"
    echo "  Duration: ${elapsed}s"
    echo ""

    # List result files.
    echo "  Result files:"
    local result_files
    result_files=$(ls -1t "$RESULTS_DIR"/*.json 2>/dev/null || true)
    if [[ -n "$result_files" ]]; then
        echo "$result_files" | while read -r f; do
            local size
            size=$(wc -c < "$f" | tr -d ' ')
            echo "    $(basename "$f")  (${size} bytes)"
        done
    else
        echo "    (none)"
    fi

    echo ""
    if $suite_ok; then
        log_info "All benchmarks passed."
    else
        log_error "Some benchmarks failed. Check output above."
        exit 1
    fi
}

main "$@"
