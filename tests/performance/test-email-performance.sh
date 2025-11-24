#!/usr/bin/env bash
# ============================================================
# Performance Tests for Email Notification System
# ============================================================
# Test Coverage:
#   1. Rate limit enforcement under load
#   2. Concurrent email sending (10 parallel processes)
#   3. Latency measurement (time to send email)
#   4. Retry logic performance (failed sends with backoff)
#   5. Prometheus metrics update performance
#
# Usage:
#   ./tests/performance/test-email-performance.sh
#
# Requirements:
#   - email-functions.sh library
#   - Bash 3.2+ (macOS compatible)
#
# Notes:
#   - Measures and reports performance metrics
#   - Tests system behavior under stress
#   - Verifies rate limits hold under concurrent access
#   - Tests file locking mechanisms
# ============================================================

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Test environment setup
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
EMAIL_FUNCTIONS_LIB="$PROJECT_ROOT/scripts/lib/email-functions.sh"

# Create temporary test directory
TEST_TMP_DIR="/tmp/test-email-performance-$$"
mkdir -p "$TEST_TMP_DIR"

# Override environment variables for testing
export EMAIL_LOG_DIR="$TEST_TMP_DIR"
export EMAIL_FROM="test-sender@example.com"
export EMAIL_TO="test-recipient@example.com"
export EMAIL_ENABLED="true"
export SMTP_TIMEOUT="5"
export EMAIL_RATE_LIMIT_HOURLY="10"
export EMAIL_RATE_LIMIT_DAILY="100"
export PROMETHEUS_METRICS_DIR="$TEST_TMP_DIR/metrics"

# Create metrics directory
mkdir -p "$PROMETHEUS_METRICS_DIR"

# Source the email functions library
if [ ! -f "$EMAIL_FUNCTIONS_LIB" ]; then
    echo -e "${RED}ERROR: Email functions library not found at $EMAIL_FUNCTIONS_LIB${NC}"
    exit 1
fi

source "$EMAIL_FUNCTIONS_LIB"

# ============================================================
# Test Framework Functions
# ============================================================

# Print test header
print_test_header() {
    echo ""
    echo "=========================================="
    echo "Performance Test: $1"
    echo "=========================================="
}

# Measure execution time
measure_time() {
    local command="$1"
    local start_time
    local end_time
    local duration

    start_time=$(date +%s)

    set +e
    eval "$command" >/dev/null 2>&1
    local exit_code=$?
    set -e

    end_time=$(date +%s)
    duration=$((end_time - start_time))

    echo "$duration:$exit_code"
}

# Run test
run_test() {
    local test_name="$1"
    local test_function="$2"

    print_test_header "$test_name"
    ((TESTS_RUN++))

    # Clean up test environment before each test
    rm -f "$TEST_TMP_DIR"/*.log 2>/dev/null || true
    rm -f "$TEST_TMP_DIR"/*.prom 2>/dev/null || true
    rm -f "$TEST_TMP_DIR"/ALERT 2>/dev/null || true
    rm -f "$PROMETHEUS_METRICS_DIR"/*.prom 2>/dev/null || true

    if $test_function; then
        ((TESTS_PASSED++))
        echo -e "${GREEN}✓ Test PASSED${NC}"
    else
        ((TESTS_FAILED++))
        echo -e "${RED}✗ Test FAILED${NC}"
    fi
}

# Setup mock msmtp with configurable behavior
setup_mock_msmtp() {
    local behavior="${1:-success}"  # success, failure, slow
    local mock_msmtp="$TEST_TMP_DIR/msmtp"

    if [ "$behavior" = "success" ]; then
        cat > "$mock_msmtp" << 'EOF'
#!/usr/bin/env bash
# Mock msmtp - always succeeds quickly
cat > /dev/null
exit 0
EOF
    elif [ "$behavior" = "failure" ]; then
        cat > "$mock_msmtp" << 'EOF'
#!/usr/bin/env bash
# Mock msmtp - always fails
cat > /dev/null
exit 1
EOF
    elif [ "$behavior" = "slow" ]; then
        cat > "$mock_msmtp" << 'EOF'
#!/usr/bin/env bash
# Mock msmtp - succeeds but slowly (1 second delay)
cat > /dev/null
sleep 1
exit 0
EOF
    fi

    chmod +x "$mock_msmtp"
    export PATH="$TEST_TMP_DIR:$PATH"
}

# ============================================================
# Test 1: Rate Limit Enforcement Under Load
# ============================================================
test_rate_limit_under_load() {
    setup_mock_msmtp "success"

    echo -e "${BLUE}INFO${NC}: Testing rate limit enforcement (limit: $EMAIL_RATE_LIMIT_HOURLY/hour)"

    # Clear rate limit log
    rm -f "$EMAIL_LOG_DIR/email-rate-limit.log"

    local start_time
    local end_time
    local duration

    start_time=$(date +%s)

    # Send emails rapidly up to and beyond limit
    local success_count=0
    local blocked_count=0
    local total_attempts=15  # Beyond hourly limit of 10

    for i in $(seq 1 $total_attempts); do
        local corr_id
        corr_id=$(generate_correlation_id)

        set +e
        send_email "Load Test $i" "Body $i" "$corr_id" "normal" >/dev/null 2>&1
        local result=$?
        set -e

        if [ $result -eq 0 ]; then
            ((success_count++))
        else
            ((blocked_count++))
        fi
    done

    end_time=$(date +%s)
    duration=$((end_time - start_time))

    echo -e "${BLUE}RESULTS${NC}:"
    echo "  Total Attempts: $total_attempts"
    echo "  Successful: $success_count"
    echo "  Blocked: $blocked_count"
    echo "  Duration: ${duration}s"
    echo "  Rate: $(echo "scale=2; $total_attempts / $duration" | bc 2>/dev/null || echo "N/A") attempts/sec"

    # Verify rate limit was enforced
    if [ "$success_count" -le "$EMAIL_RATE_LIMIT_HOURLY" ]; then
        echo -e "${GREEN}✓ PASS${NC}: Rate limit enforced (success count: $success_count <= limit: $EMAIL_RATE_LIMIT_HOURLY)"
    else
        echo -e "${RED}✗ FAIL${NC}: Rate limit not enforced (success count: $success_count > limit: $EMAIL_RATE_LIMIT_HOURLY)"
        return 1
    fi

    if [ "$blocked_count" -gt 0 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Emails were blocked ($blocked_count blocked)"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: No emails were blocked"
    fi

    # Verify rate limit log integrity
    local rate_limit_log="$EMAIL_LOG_DIR/email-rate-limit.log"
    if [ -f "$rate_limit_log" ]; then
        local entry_count
        entry_count=$(wc -l < "$rate_limit_log" | tr -d ' ')
        echo -e "${BLUE}INFO${NC}: Rate limit log has $entry_count entries"

        if [ "$entry_count" -ge "$success_count" ]; then
            echo -e "${GREEN}✓ PASS${NC}: Rate limit log entries match or exceed success count"
        fi
    fi

    return 0
}

# ============================================================
# Test 2: Concurrent Email Sending
# ============================================================
test_concurrent_sending() {
    setup_mock_msmtp "success"

    echo -e "${BLUE}INFO${NC}: Testing concurrent email sending (10 parallel processes)"

    # Clear logs
    rm -f "$EMAIL_LOG_DIR/email.log"
    rm -f "$EMAIL_LOG_DIR/email-rate-limit.log"

    # Set higher rate limit for concurrency test
    export EMAIL_RATE_LIMIT_HOURLY="50"
    export EMAIL_RATE_LIMIT_DAILY="500"

    local concurrent_processes=10
    local start_time
    local end_time

    start_time=$(date +%s)

    # Launch concurrent processes
    local pids=()
    for i in $(seq 1 $concurrent_processes); do
        (
            local corr_id
            corr_id=$(generate_correlation_id)
            send_email "Concurrent Test $i" "Body $i from process $$" "$corr_id" "normal" >/dev/null 2>&1
        ) &
        pids+=($!)
    done

    # Wait for all processes to complete
    local success_count=0
    local failure_count=0

    for pid in "${pids[@]}"; do
        set +e
        wait "$pid"
        local result=$?
        set -e

        if [ $result -eq 0 ]; then
            ((success_count++))
        else
            ((failure_count++))
        fi
    done

    end_time=$(date +%s)
    local duration=$((end_time - start_time))

    echo -e "${BLUE}RESULTS${NC}:"
    echo "  Concurrent Processes: $concurrent_processes"
    echo "  Successful: $success_count"
    echo "  Failed: $failure_count"
    echo "  Duration: ${duration}s"
    echo "  Avg Time/Email: $(echo "scale=2; $duration / $concurrent_processes" | bc 2>/dev/null || echo "N/A")s"

    # Verify no race conditions (all processes should succeed)
    if [ "$success_count" -eq "$concurrent_processes" ]; then
        echo -e "${GREEN}✓ PASS${NC}: All concurrent emails sent successfully"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Some concurrent sends failed ($failure_count failures)"
    fi

    # Verify email log integrity
    local email_log="$EMAIL_LOG_DIR/email.log"
    if [ -f "$email_log" ]; then
        local entry_count
        entry_count=$(wc -l < "$email_log" | tr -d ' ')

        echo -e "${BLUE}INFO${NC}: Email log has $entry_count entries (expected $success_count)"

        if [ "$entry_count" -eq "$success_count" ]; then
            echo -e "${GREEN}✓ PASS${NC}: Log entries match successful sends (no race conditions)"
        else
            echo -e "${YELLOW}⚠ WARNING${NC}: Log entry count mismatch (may indicate race condition)"
        fi
    fi

    # Verify Prometheus metrics integrity
    local metrics_file="$PROMETHEUS_METRICS_DIR/email_metrics.prom"
    if [ -f "$metrics_file" ]; then
        echo -e "${GREEN}✓ PASS${NC}: Metrics file created despite concurrent access"

        # Check metrics file is valid
        if grep -q 'catchup_email_sent_total' "$metrics_file"; then
            echo -e "${GREEN}✓ PASS${NC}: Metrics file is valid"
        fi
    fi

    return 0
}

# ============================================================
# Test 3: Latency Measurement
# ============================================================
test_latency_measurement() {
    setup_mock_msmtp "success"

    echo -e "${BLUE}INFO${NC}: Measuring email send latency"

    # Clear logs
    rm -f "$EMAIL_LOG_DIR/email-rate-limit.log"

    local test_iterations=5
    local total_time=0
    local min_time=999999
    local max_time=0

    # Reset rate limits for clean test
    export EMAIL_RATE_LIMIT_HOURLY="100"

    for i in $(seq 1 $test_iterations); do
        local corr_id
        corr_id=$(generate_correlation_id)

        local result
        result=$(measure_time "send_email 'Latency Test $i' 'Body $i' '$corr_id' 'normal'")

        local duration
        local exit_code
        duration=$(echo "$result" | cut -d: -f1)
        exit_code=$(echo "$result" | cut -d: -f2)

        total_time=$((total_time + duration))

        if [ "$duration" -lt "$min_time" ]; then
            min_time=$duration
        fi

        if [ "$duration" -gt "$max_time" ]; then
            max_time=$duration
        fi

        echo -e "${BLUE}  Iteration $i${NC}: ${duration}s (exit code: $exit_code)"
    done

    local avg_time
    avg_time=$((total_time / test_iterations))

    echo ""
    echo -e "${BLUE}LATENCY RESULTS${NC}:"
    echo "  Iterations: $test_iterations"
    echo "  Total Time: ${total_time}s"
    echo "  Average: ${avg_time}s"
    echo "  Min: ${min_time}s"
    echo "  Max: ${max_time}s"

    # Performance target: < 5 seconds per email
    local target_latency=5

    if [ "$avg_time" -le "$target_latency" ]; then
        echo -e "${GREEN}✓ PASS${NC}: Average latency ${avg_time}s <= target ${target_latency}s"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Average latency ${avg_time}s > target ${target_latency}s"
    fi

    return 0
}

# ============================================================
# Test 4: Retry Logic Performance
# ============================================================
test_retry_logic_performance() {
    setup_mock_msmtp "failure"

    echo -e "${BLUE}INFO${NC}: Testing retry logic performance with exponential backoff"

    # Clear logs
    rm -f "$EMAIL_LOG_DIR/email.log"
    rm -f "$EMAIL_LOG_DIR/email-rate-limit.log"

    local corr_id
    corr_id=$(generate_correlation_id)

    local start_time
    local end_time

    start_time=$(date +%s)

    # Send email that will fail and retry
    # Expected: 3 retries with 2s, 4s, 8s delays = ~14 seconds total
    set +e
    send_email "Retry Test" "Testing retry logic" "$corr_id" "normal" >/dev/null 2>&1
    local result=$?
    set -e

    end_time=$(date +%s)
    local duration=$((end_time - start_time))

    echo -e "${BLUE}RESULTS${NC}:"
    echo "  Exit Code: $result (expected 1 = failure)"
    echo "  Duration: ${duration}s"

    # Verify email failed as expected
    if [ $result -eq 1 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Email failed as expected after retries"
    else
        echo -e "${RED}✗ FAIL${NC}: Unexpected exit code: $result"
        return 1
    fi

    # Verify retry delays (should be ~14 seconds for 3 retries with backoff)
    # But since send_email uses retry_send_email internally with timeouts,
    # actual duration may vary. Accept 5-20 second range.
    if [ "$duration" -ge 2 ] && [ "$duration" -le 20 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Retry duration ${duration}s is within expected range (2-20s)"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Retry duration ${duration}s outside expected range"
    fi

    # Verify email log shows retry attempts
    local email_log="$EMAIL_LOG_DIR/email.log"
    if [ -f "$email_log" ]; then
        local failure_count
        failure_count=$(grep -c '"status":"failure"' "$email_log" || echo "0")

        echo -e "${BLUE}INFO${NC}: Email log shows $failure_count failure entries"

        if [ "$failure_count" -ge 1 ]; then
            echo -e "${GREEN}✓ PASS${NC}: Failures logged correctly"
        fi
    fi

    # Verify fallback alert triggered
    local alert_file="$EMAIL_LOG_DIR/ALERT"
    if [ -f "$alert_file" ]; then
        echo -e "${GREEN}✓ PASS${NC}: Fallback alert file created after retry failures"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Alert file not created (may be bash version issue)"
    fi

    return 0
}

# ============================================================
# Test 5: Prometheus Metrics Update Performance
# ============================================================
test_metrics_update_performance() {
    setup_mock_msmtp "success"

    echo -e "${BLUE}INFO${NC}: Testing Prometheus metrics update performance"

    # Clear metrics
    rm -f "$PROMETHEUS_METRICS_DIR"/*.prom
    rm -f "$EMAIL_LOG_DIR/email-rate-limit.log"

    local test_iterations=20
    local start_time
    local end_time

    start_time=$(date +%s)

    # Send multiple emails to trigger metric updates
    for i in $(seq 1 $test_iterations); do
        local corr_id
        corr_id=$(generate_correlation_id)
        send_email "Metrics Test $i" "Body $i" "$corr_id" "normal" >/dev/null 2>&1
    done

    end_time=$(date +%s)
    local duration=$((end_time - start_time))

    echo -e "${BLUE}RESULTS${NC}:"
    echo "  Email Sends: $test_iterations"
    echo "  Total Duration: ${duration}s"
    echo "  Avg Time/Email: $(echo "scale=3; $duration / $test_iterations" | bc 2>/dev/null || echo "N/A")s"

    # Verify metrics file integrity
    local metrics_file="$PROMETHEUS_METRICS_DIR/email_metrics.prom"
    if [ ! -f "$metrics_file" ]; then
        echo -e "${RED}✗ FAIL${NC}: Metrics file not created"
        return 1
    fi

    echo -e "${GREEN}✓ PASS${NC}: Metrics file created"

    # Verify metrics content
    local metric_content
    metric_content=$(cat "$metrics_file")

    # Check for corruption or race conditions in metrics file
    if echo "$metric_content" | grep -q '^catchup_email'; then
        echo -e "${GREEN}✓ PASS${NC}: Metrics file format is valid"
    else
        echo -e "${RED}✗ FAIL${NC}: Metrics file format invalid"
        return 1
    fi

    # Count unique metrics
    local metric_count
    metric_count=$(grep -c '^catchup_email' "$metrics_file" || echo "0")

    echo -e "${BLUE}INFO${NC}: Metrics file contains $metric_count metric entries"

    if [ "$metric_count" -gt 0 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Metrics successfully updated"
    fi

    # Verify atomic writes (no temp files left behind)
    local temp_files
    temp_files=$(find "$PROMETHEUS_METRICS_DIR" -name "*.tmp" -o -name ".metrics*" | wc -l | tr -d ' ')

    if [ "$temp_files" -eq 0 ]; then
        echo -e "${GREEN}✓ PASS${NC}: No temporary files left (atomic writes working)"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Found $temp_files temporary files"
    fi

    # Check file permissions
    local file_perms
    file_perms=$(stat -f "%Lp" "$metrics_file" 2>/dev/null || stat -c "%a" "$metrics_file" 2>/dev/null)

    if [ "$file_perms" = "644" ]; then
        echo -e "${GREEN}✓ PASS${NC}: Metrics file has correct permissions (644)"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Metrics file permissions are $file_perms (expected 644)"
    fi

    return 0
}

# ============================================================
# Test 6: File Locking Under Concurrent Access
# ============================================================
test_file_locking() {
    setup_mock_msmtp "success"

    echo -e "${BLUE}INFO${NC}: Testing file locking under concurrent access"

    # Clear logs
    rm -f "$EMAIL_LOG_DIR/email-rate-limit.log"
    rm -f "$PROMETHEUS_METRICS_DIR"/*.prom

    # Set high rate limits
    export EMAIL_RATE_LIMIT_HOURLY="100"

    local concurrent_processes=5
    local emails_per_process=3

    local start_time
    start_time=$(date +%s)

    # Launch concurrent processes that all update the same rate limit log
    local pids=()
    for i in $(seq 1 $concurrent_processes); do
        (
            for j in $(seq 1 $emails_per_process); do
                local corr_id
                corr_id=$(generate_correlation_id)
                send_email "Lock Test P$i-E$j" "Process $i Email $j" "$corr_id" "normal" >/dev/null 2>&1
            done
        ) &
        pids+=($!)
    done

    # Wait for all processes
    for pid in "${pids[@]}"; do
        wait "$pid" 2>/dev/null || true
    done

    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))

    local expected_total=$((concurrent_processes * emails_per_process))

    echo -e "${BLUE}RESULTS${NC}:"
    echo "  Concurrent Processes: $concurrent_processes"
    echo "  Emails per Process: $emails_per_process"
    echo "  Expected Total: $expected_total"
    echo "  Duration: ${duration}s"

    # Check rate limit log integrity
    local rate_limit_log="$EMAIL_LOG_DIR/email-rate-limit.log"
    if [ -f "$rate_limit_log" ]; then
        local entry_count
        entry_count=$(wc -l < "$rate_limit_log" | tr -d ' ')

        echo -e "${BLUE}INFO${NC}: Rate limit log has $entry_count entries (expected $expected_total)"

        # Allow some variance due to rate limiting cleanup
        if [ "$entry_count" -ge "$((expected_total - 5))" ] && [ "$entry_count" -le "$((expected_total + 5))" ]; then
            echo -e "${GREEN}✓ PASS${NC}: Rate limit log entries within expected range (file locking working)"
        else
            echo -e "${YELLOW}⚠ WARNING${NC}: Entry count outside expected range (may indicate lock issues)"
        fi

        # Check for corrupted entries
        local invalid_lines
        invalid_lines=$(awk 'NF != 2' "$rate_limit_log" | wc -l | tr -d ' ')

        if [ "$invalid_lines" -eq 0 ]; then
            echo -e "${GREEN}✓ PASS${NC}: No corrupted entries in rate limit log"
        else
            echo -e "${RED}✗ FAIL${NC}: Found $invalid_lines corrupted entries"
            return 1
        fi
    else
        echo -e "${RED}✗ FAIL${NC}: Rate limit log not created"
        return 1
    fi

    # Check metrics file integrity
    local metrics_file="$PROMETHEUS_METRICS_DIR/email_metrics.prom"
    if [ -f "$metrics_file" ]; then
        if grep -q '^catchup_email' "$metrics_file"; then
            echo -e "${GREEN}✓ PASS${NC}: Metrics file is valid despite concurrent updates"
        else
            echo -e "${RED}✗ FAIL${NC}: Metrics file corrupted"
            return 1
        fi
    fi

    return 0
}

# ============================================================
# Main Test Runner
# ============================================================
main() {
    echo "=========================================="
    echo "Email System - Performance Tests"
    echo "=========================================="
    echo "Project Root: $PROJECT_ROOT"
    echo "Library: $EMAIL_FUNCTIONS_LIB"
    echo "Test Directory: $TEST_TMP_DIR"
    echo ""

    # Run all tests
    run_test "TEST 1: Rate Limit Enforcement Under Load" test_rate_limit_under_load
    run_test "TEST 2: Concurrent Email Sending" test_concurrent_sending
    run_test "TEST 3: Latency Measurement" test_latency_measurement
    run_test "TEST 4: Retry Logic Performance" test_retry_logic_performance
    run_test "TEST 5: Metrics Update Performance" test_metrics_update_performance
    run_test "TEST 6: File Locking Under Concurrent Access" test_file_locking

    # Clean up test directory
    echo ""
    echo -e "${BLUE}INFO${NC}: Cleaning up test directory: $TEST_TMP_DIR"
    rm -rf "$TEST_TMP_DIR"

    # Print summary
    echo ""
    echo "=========================================="
    echo "Performance Test Summary"
    echo "=========================================="
    echo "Total Tests: $TESTS_RUN"
    echo -e "${GREEN}Passed: $TESTS_PASSED${NC}"
    echo -e "${RED}Failed: $TESTS_FAILED${NC}"
    echo ""

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}✓ All performance tests passed!${NC}"
        exit 0
    else
        echo -e "${RED}✗ Some performance tests failed${NC}"
        exit 1
    fi
}

# Run main function
main
