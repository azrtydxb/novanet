#!/usr/bin/env bash
# run-all.sh — Run all NovaNet integration tests and report a summary.
#
# Usage:
#   ./run-all.sh              # Run all tests
#   ./run-all.sh 01 05 07     # Run specific tests by number
#   ./run-all.sh --skip 03 04 # Skip specific tests
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

log_header "NovaNet Integration Test Suite"

###############################################################################
# Parse arguments
###############################################################################
SKIP_MODE=false
SELECTED_TESTS=()
SKIP_TESTS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip)
            SKIP_MODE=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [--skip] [test_numbers...]"
            echo ""
            echo "Examples:"
            echo "  $0              # Run all tests"
            echo "  $0 01 05 07     # Run only tests 01, 05, 07"
            echo "  $0 --skip 03 04 # Skip tests 03 and 04"
            exit 0
            ;;
        *)
            if $SKIP_MODE; then
                SKIP_TESTS+=("$1")
            else
                SELECTED_TESTS+=("$1")
            fi
            shift
            ;;
    esac
done

###############################################################################
# Discover test scripts
###############################################################################
ALL_TESTS=()
for script in "$SCRIPT_DIR"/[0-9][0-9]-*.sh; do
    [[ -f "$script" ]] || continue
    ALL_TESTS+=("$script")
done

if [[ ${#ALL_TESTS[@]} -eq 0 ]]; then
    log_fail "No test scripts found in $SCRIPT_DIR"
    exit 1
fi

log_info "Found ${#ALL_TESTS[@]} test script(s)"

# Filter tests based on arguments
TESTS_TO_RUN=()
for script in "${ALL_TESTS[@]}"; do
    basename="$(basename "$script")"
    test_num="${basename%%-*}"  # e.g., "01" from "01-same-node.sh"

    if [[ ${#SELECTED_TESTS[@]} -gt 0 ]]; then
        # Only run selected tests
        for sel in "${SELECTED_TESTS[@]}"; do
            if [[ "$test_num" == "$sel" ]]; then
                TESTS_TO_RUN+=("$script")
                break
            fi
        done
    elif [[ ${#SKIP_TESTS[@]} -gt 0 ]]; then
        # Skip specified tests
        skip=false
        for s in "${SKIP_TESTS[@]}"; do
            if [[ "$test_num" == "$s" ]]; then
                skip=true
                break
            fi
        done
        if ! $skip; then
            TESTS_TO_RUN+=("$script")
        else
            log_info "Skipping: $basename"
        fi
    else
        TESTS_TO_RUN+=("$script")
    fi
done

if [[ ${#TESTS_TO_RUN[@]} -eq 0 ]]; then
    log_fail "No tests selected to run"
    exit 1
fi

log_info "Running ${#TESTS_TO_RUN[@]} test(s)"
echo ""

###############################################################################
# Preflight
###############################################################################
preflight_check

###############################################################################
# Run tests
###############################################################################
SUITE_PASSED=0
SUITE_FAILED=0
SUITE_SKIPPED=0
RESULTS=()
START_TIME="$(date +%s)"

for script in "${TESTS_TO_RUN[@]}"; do
    basename="$(basename "$script")"
    log_header "Running: $basename"

    test_start="$(date +%s)"
    set +e
    bash "$script"
    exit_code=$?
    set -e
    test_end="$(date +%s)"
    test_duration=$(( test_end - test_start ))

    if [[ $exit_code -eq 0 ]]; then
        SUITE_PASSED=$(( SUITE_PASSED + 1 ))
        RESULTS+=("${GREEN}PASS${NC}  ${basename}  (${test_duration}s)")
    else
        SUITE_FAILED=$(( SUITE_FAILED + 1 ))
        RESULTS+=("${RED}FAIL${NC}  ${basename}  (${test_duration}s)")
    fi
    echo ""
done

END_TIME="$(date +%s)"
TOTAL_DURATION=$(( END_TIME - START_TIME ))

###############################################################################
# Summary
###############################################################################
log_header "Integration Test Suite Summary"

echo -e "${BOLD}Results:${NC}"
for result in "${RESULTS[@]}"; do
    echo -e "  $result"
done
echo ""

echo -e "${BOLD}Totals:${NC}"
echo -e "  ${GREEN}Passed:${NC}  $SUITE_PASSED"
echo -e "  ${RED}Failed:${NC}  $SUITE_FAILED"
echo -e "  Total time: ${TOTAL_DURATION}s"
echo ""

if [[ $SUITE_FAILED -gt 0 ]]; then
    echo -e "${RED}${BOLD}SUITE RESULT: FAILED${NC} ($SUITE_FAILED of ${#TESTS_TO_RUN[@]} test scripts failed)"
    exit 1
else
    echo -e "${GREEN}${BOLD}SUITE RESULT: PASSED${NC} (${SUITE_PASSED}/${#TESTS_TO_RUN[@]} test scripts passed)"
    exit 0
fi
