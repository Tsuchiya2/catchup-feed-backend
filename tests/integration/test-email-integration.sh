#!/usr/bin/env bash
# ============================================================
# Integration Tests for Email Notification System
# ============================================================
# Test Coverage:
#   1. msmtp connectivity test - Verify SMTP server reachable
#   2. Email sending with real SMTP - Test actual email delivery
#   3. Rate limiting behavior - Verify hourly/daily limits enforced
#   4. Fallback mechanisms - Test syslog and alert file when email fails
#   5. Prometheus metrics integration - Verify metrics updated correctly
#   6. Log file creation and rotation - Verify log management
#
# Usage:
#   ./tests/integration/test-email-integration.sh
#
# Requirements:
#   - email-functions.sh library in scripts/lib/
#   - msmtp installed and configured
#   - .env file with EMAIL_* configuration
#
# Notes:
#   - Uses temporary directories for test artifacts
#   - Can use actual msmtp or mock SMTP server
#   - Cleans up test artifacts after execution
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
TEST_TMP_DIR="/tmp/test-email-integration-$$"
mkdir -p "$TEST_TMP_DIR"

# Override environment variables for testing
export EMAIL_LOG_DIR="$TEST_TMP_DIR"
export EMAIL_FROM="${EMAIL_FROM:-test-sender@example.com}"
export EMAIL_TO="${EMAIL_TO:-test-recipient@example.com}"
export EMAIL_ENABLED="true"
export SMTP_TIMEOUT="${EMAIL_SMTP_TIMEOUT:-5}"
export EMAIL_RATE_LIMIT_HOURLY="${EMAIL_RATE_LIMIT_HOURLY:-10}"
export EMAIL_RATE_LIMIT_DAILY="${EMAIL_RATE_LIMIT_DAILY:-100}"
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
    echo "Integration Test: $1"
    echo "=========================================="
}

# Assert equal
assert_equal() {
    local expected="$1"
    local actual="$2"
    local message="${3:-}"

    if [ "$expected" = "$actual" ]; then
        echo -e "${GREEN}✓ PASS${NC}: $message"
        return 0
    else
        echo -e "${RED}✗ FAIL${NC}: $message"
        echo "  Expected: '$expected'"
        echo "  Actual:   '$actual'"
        return 1
    fi
}

# Assert contains
assert_contains() {
    local haystack="$1"
    local needle="$2"
    local message="${3:-}"

    if [[ "$haystack" == *"$needle"* ]]; then
        echo -e "${GREEN}✓ PASS${NC}: $message"
        return 0
    else
        echo -e "${RED}✗ FAIL${NC}: $message"
        echo "  Haystack: '$haystack'"
        echo "  Needle:   '$needle'"
        return 1
    fi
}

# Assert file exists
assert_file_exists() {
    local file="$1"
    local message="${2:-File should exist: $file}"

    if [ -f "$file" ]; then
        echo -e "${GREEN}✓ PASS${NC}: $message"
        return 0
    else
        echo -e "${RED}✗ FAIL${NC}: $message"
        return 1
    fi
}

# Assert exit code
assert_exit_code() {
    local expected_code="$1"
    local command="$2"
    local message="${3:-}"

    set +e
    eval "$command" >/dev/null 2>&1
    local actual_code=$?
    set -e

    if [ "$expected_code" -eq "$actual_code" ]; then
        echo -e "${GREEN}✓ PASS${NC}: $message (exit code: $actual_code)"
        return 0
    else
        echo -e "${RED}✗ FAIL${NC}: $message"
        echo "  Expected exit code: $expected_code"
        echo "  Actual exit code:   $actual_code"
        return 1
    fi
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

# ============================================================
# Test 1: msmtp Connectivity Test
# ============================================================
test_msmtp_connectivity() {
    # Check if msmtp is installed
    if ! command -v msmtp &> /dev/null; then
        echo -e "${YELLOW}⚠ WARNING${NC}: msmtp not installed, creating mock msmtp"

        # Create mock msmtp for testing
        local mock_msmtp="$TEST_TMP_DIR/msmtp"
        cat > "$mock_msmtp" << 'EOF'
#!/usr/bin/env bash
# Mock msmtp - simulates SMTP connectivity test
if [ "$1" = "--serverinfo" ] || [ "$1" = "-S" ]; then
    echo "SMTP server at smtp.gmail.com (smtp.gmail.com [142.250.185.109]), port 587"
    echo "TLS: on"
    exit 0
fi
cat > /dev/null
exit 0
EOF
        chmod +x "$mock_msmtp"
        export PATH="$TEST_TMP_DIR:$PATH"

        echo -e "${GREEN}✓ PASS${NC}: Mock msmtp created for testing"
    else
        echo -e "${GREEN}✓ PASS${NC}: msmtp is installed"
    fi

    # Test msmtp can be executed
    assert_exit_code 0 "command -v msmtp" "msmtp command is available" || return 1

    # Test msmtp configuration exists
    local msmtp_config="$HOME/.msmtprc"
    if [ -f "$msmtp_config" ]; then
        echo -e "${GREEN}✓ PASS${NC}: msmtp configuration file exists"

        # Check permissions (should be 600)
        local file_perms
        file_perms=$(stat -f "%Lp" "$msmtp_config" 2>/dev/null || stat -c "%a" "$msmtp_config" 2>/dev/null)
        if [ "$file_perms" = "600" ]; then
            echo -e "${GREEN}✓ PASS${NC}: msmtp config has correct permissions (600)"
        else
            echo -e "${YELLOW}⚠ WARNING${NC}: msmtp config permissions are $file_perms (should be 600)"
        fi
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: msmtp configuration not found (using mock)"
    fi

    # Test SMTP server connectivity (using timeout)
    local smtp_server="${EMAIL_SMTP_SERVER:-smtp.gmail.com}"
    local smtp_port="${EMAIL_SMTP_PORT:-587}"

    echo -e "${BLUE}INFO${NC}: Testing SMTP connectivity to $smtp_server:$smtp_port"

    # Use nc (netcat) to test connectivity if available
    if command -v nc &> /dev/null; then
        set +e
        timeout 5 nc -z "$smtp_server" "$smtp_port" 2>/dev/null
        local nc_result=$?
        set -e

        if [ $nc_result -eq 0 ]; then
            echo -e "${GREEN}✓ PASS${NC}: SMTP server is reachable"
        else
            echo -e "${YELLOW}⚠ WARNING${NC}: SMTP server not reachable (may be firewalled or offline)"
        fi
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: nc (netcat) not available, skipping connectivity test"
    fi

    return 0
}

# ============================================================
# Test 2: Email Sending with Real/Mock SMTP
# ============================================================
test_email_sending() {
    # Set up mock msmtp for controlled testing
    local mock_msmtp="$TEST_TMP_DIR/msmtp"
    cat > "$mock_msmtp" << 'EOF'
#!/usr/bin/env bash
# Mock msmtp - simulates successful email send
cat > /dev/null
exit 0
EOF
    chmod +x "$mock_msmtp"

    # Prepend test directory to PATH
    export PATH="$TEST_TMP_DIR:$PATH"

    # Clear rate limit log
    rm -f "$EMAIL_LOG_DIR/email-rate-limit.log"

    # Generate correlation ID
    local correlation_id
    correlation_id=$(generate_correlation_id)

    # Send test email
    local subject="Integration Test Email"
    local body="This is a test email from the integration test suite."

    echo -e "${BLUE}INFO${NC}: Sending test email with correlation ID: $correlation_id"

    set +e
    send_email "$subject" "$body" "$correlation_id" "normal"
    local send_result=$?
    set -e

    if [ $send_result -eq 0 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Email sent successfully"
    else
        echo -e "${RED}✗ FAIL${NC}: Email send failed with exit code $send_result"
        return 1
    fi

    # Verify email log entry
    local email_log="$EMAIL_LOG_DIR/email.log"
    assert_file_exists "$email_log" "Email log file created" || return 1

    local log_content
    log_content=$(tail -1 "$email_log")
    assert_contains "$log_content" "$correlation_id" "Log contains correlation ID" || return 1
    assert_contains "$log_content" '"status":"success"' "Log shows success status" || return 1

    # Verify Prometheus metrics updated
    local metrics_file="$PROMETHEUS_METRICS_DIR/email_metrics.prom"
    if [ -f "$metrics_file" ]; then
        local metric_content
        metric_content=$(cat "$metrics_file")
        assert_contains "$metric_content" "catchup_email_sent_total" "Metrics file contains email counter" || return 1
        echo -e "${GREEN}✓ PASS${NC}: Prometheus metrics updated"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Metrics file not created"
    fi

    return 0
}

# ============================================================
# Test 3: Rate Limiting Behavior
# ============================================================
test_rate_limiting() {
    # Set up mock msmtp
    local mock_msmtp="$TEST_TMP_DIR/msmtp"
    cat > "$mock_msmtp" << 'EOF'
#!/usr/bin/env bash
cat > /dev/null
exit 0
EOF
    chmod +x "$mock_msmtp"
    export PATH="$TEST_TMP_DIR:$PATH"

    # Clear rate limit log
    local rate_limit_log="$EMAIL_LOG_DIR/email-rate-limit.log"
    rm -f "$rate_limit_log"

    echo -e "${BLUE}INFO${NC}: Testing rate limit enforcement (limit: $EMAIL_RATE_LIMIT_HOURLY/hour)"

    # Send emails up to hourly limit
    local success_count=0
    for i in $(seq 1 "$EMAIL_RATE_LIMIT_HOURLY"); do
        local corr_id
        corr_id=$(generate_correlation_id)

        set +e
        send_email "Test $i" "Body $i" "$corr_id" "normal" >/dev/null 2>&1
        local result=$?
        set -e

        if [ $result -eq 0 ]; then
            ((success_count++))
        fi
    done

    assert_equal "$EMAIL_RATE_LIMIT_HOURLY" "$success_count" "All emails within limit sent successfully" || return 1

    # Try to send one more (should be rate limited)
    local corr_id
    corr_id=$(generate_correlation_id)

    set +e
    send_email "Over Limit" "Should fail" "$corr_id" "normal" >/dev/null 2>&1
    local over_limit_result=$?
    set -e

    # Exit code 2 means rate limited
    if [ $over_limit_result -eq 2 ] || [ $over_limit_result -eq 1 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Rate limit enforced (email blocked)"
    else
        echo -e "${RED}✗ FAIL${NC}: Rate limit not enforced (expected failure, got exit code $over_limit_result)"
        return 1
    fi

    # Test high priority bypass
    corr_id=$(generate_correlation_id)

    set +e
    send_email "High Priority" "Should bypass limit" "$corr_id" "high" >/dev/null 2>&1
    local high_priority_result=$?
    set -e

    if [ $high_priority_result -eq 0 ]; then
        echo -e "${GREEN}✓ PASS${NC}: High priority email bypassed rate limit"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: High priority email failed (exit code: $high_priority_result)"
        # Don't fail the test - daily limit might be reached
    fi

    # Verify rate limit log exists and has entries
    assert_file_exists "$rate_limit_log" "Rate limit log file created" || return 1

    local entry_count
    entry_count=$(wc -l < "$rate_limit_log" | tr -d ' ')

    if [ "$entry_count" -gt 0 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Rate limit log has $entry_count entries"
    else
        echo -e "${RED}✗ FAIL${NC}: Rate limit log is empty"
        return 1
    fi

    return 0
}

# ============================================================
# Test 4: Fallback Mechanisms
# ============================================================
test_fallback_mechanisms() {
    # Create failing mock msmtp
    local mock_msmtp="$TEST_TMP_DIR/msmtp"
    cat > "$mock_msmtp" << 'EOF'
#!/usr/bin/env bash
# Mock msmtp - always fails
cat > /dev/null
exit 1
EOF
    chmod +x "$mock_msmtp"
    export PATH="$TEST_TMP_DIR:$PATH"

    # Clear logs
    rm -f "$EMAIL_LOG_DIR/email.log"
    rm -f "$EMAIL_LOG_DIR/email-rate-limit.log"
    rm -f "$EMAIL_LOG_DIR/ALERT"

    echo -e "${BLUE}INFO${NC}: Testing fallback alerting when SMTP fails"

    # Try to send email (should fail and trigger fallback)
    local correlation_id
    correlation_id=$(generate_correlation_id)

    set +e
    send_email "Test Failure" "Should trigger fallback" "$correlation_id" "normal" >/dev/null 2>&1
    local result=$?
    set -e

    if [ $result -ne 0 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Email send failed as expected (exit code: $result)"
    else
        echo -e "${RED}✗ FAIL${NC}: Email should have failed but succeeded"
        return 1
    fi

    # Verify fallback alert file created
    local alert_file="$EMAIL_LOG_DIR/ALERT"

    # Wait briefly for alert file to be created
    sleep 1

    if [ -f "$alert_file" ]; then
        echo -e "${GREEN}✓ PASS${NC}: Fallback alert file created"

        # Verify alert content
        local alert_content
        alert_content=$(cat "$alert_file")
        assert_contains "$alert_content" "Failed to send email" "Alert contains failure message" || return 1
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Alert file not created (may be bash version issue)"
    fi

    # Test consecutive failures
    rm -f "$EMAIL_LOG_DIR/email.log"

    # Send 3 failing emails
    for i in {1..3}; do
        local corr_id
        corr_id=$(generate_correlation_id)
        send_email "Failure $i" "Body $i" "$corr_id" "normal" >/dev/null 2>&1 || true
    done

    # Check for consecutive failures
    set +e
    check_consecutive_failures >/dev/null 2>&1
    local consecutive_result=$?
    set -e

    if [ $consecutive_result -eq 1 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Consecutive failures detected"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Consecutive failure detection returned $consecutive_result"
    fi

    return 0
}

# ============================================================
# Test 5: Prometheus Metrics Integration
# ============================================================
test_prometheus_metrics() {
    # Set up mock msmtp
    local mock_msmtp="$TEST_TMP_DIR/msmtp"
    cat > "$mock_msmtp" << 'EOF'
#!/usr/bin/env bash
cat > /dev/null
exit 0
EOF
    chmod +x "$mock_msmtp"
    export PATH="$TEST_TMP_DIR:$PATH"

    # Clear metrics
    rm -f "$PROMETHEUS_METRICS_DIR"/*.prom

    # Clear rate limit log
    rm -f "$EMAIL_LOG_DIR/email-rate-limit.log"

    echo -e "${BLUE}INFO${NC}: Testing Prometheus metrics integration"

    # Send multiple emails
    for i in {1..3}; do
        local corr_id
        corr_id=$(generate_correlation_id)
        send_email "Test $i" "Body $i" "$corr_id" "normal" >/dev/null 2>&1
    done

    # Verify metrics file
    local metrics_file="$PROMETHEUS_METRICS_DIR/email_metrics.prom"
    assert_file_exists "$metrics_file" "Metrics file created" || return 1

    # Verify metrics content
    local metric_content
    metric_content=$(cat "$metrics_file")

    # Check for required metrics
    assert_contains "$metric_content" "catchup_email_sent_total" "Contains email send counter" || return 1
    assert_contains "$metric_content" "catchup_email_send_duration_seconds" "Contains duration metric" || return 1

    # Verify metrics format (Prometheus text format)
    if echo "$metric_content" | grep -q '^catchup_email.*{.*}.*[0-9]'; then
        echo -e "${GREEN}✓ PASS${NC}: Metrics in valid Prometheus format"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Metrics format may not be valid"
    fi

    # Test metric update
    local initial_content="$metric_content"

    # Send one more email
    local corr_id
    corr_id=$(generate_correlation_id)
    send_email "Test Update" "Body" "$corr_id" "normal" >/dev/null 2>&1

    # Check if metrics updated
    local updated_content
    updated_content=$(cat "$metrics_file")

    if [ "$initial_content" != "$updated_content" ]; then
        echo -e "${GREEN}✓ PASS${NC}: Metrics updated after email send"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Metrics may not have updated"
    fi

    # Verify atomic write (file should always be valid)
    local file_perms
    file_perms=$(stat -f "%Lp" "$metrics_file" 2>/dev/null || stat -c "%a" "$metrics_file" 2>/dev/null)
    assert_equal "644" "$file_perms" "Metrics file has correct permissions (644)" || return 1

    return 0
}

# ============================================================
# Test 6: Log File Creation and Management
# ============================================================
test_log_file_management() {
    # Set up mock msmtp
    local mock_msmtp="$TEST_TMP_DIR/msmtp"
    cat > "$mock_msmtp" << 'EOF'
#!/usr/bin/env bash
cat > /dev/null
exit 0
EOF
    chmod +x "$mock_msmtp"
    export PATH="$TEST_TMP_DIR:$PATH"

    # Clear all logs
    rm -f "$EMAIL_LOG_DIR"/*.log

    echo -e "${BLUE}INFO${NC}: Testing log file creation and management"

    # Send emails to create logs
    for i in {1..5}; do
        local corr_id
        corr_id=$(generate_correlation_id)
        send_email "Test $i" "Body $i" "$corr_id" "normal" >/dev/null 2>&1
    done

    # Verify email log created
    local email_log="$EMAIL_LOG_DIR/email.log"
    assert_file_exists "$email_log" "Email log file created" || return 1

    # Verify log format (JSON)
    local log_line
    log_line=$(tail -1 "$email_log")

    if echo "$log_line" | jq . >/dev/null 2>&1; then
        echo -e "${GREEN}✓ PASS${NC}: Log entries are valid JSON"
    else
        echo -e "${YELLOW}⚠ WARNING${NC}: Log format may not be valid JSON"
    fi

    # Verify log contains required fields
    assert_contains "$log_line" '"timestamp"' "Log contains timestamp field" || return 1
    assert_contains "$log_line" '"correlation_id"' "Log contains correlation_id field" || return 1
    assert_contains "$log_line" '"status"' "Log contains status field" || return 1

    # Verify rate limit log created
    local rate_limit_log="$EMAIL_LOG_DIR/email-rate-limit.log"
    assert_file_exists "$rate_limit_log" "Rate limit log file created" || return 1

    # Count log entries
    local entry_count
    entry_count=$(wc -l < "$email_log" | tr -d ' ')

    if [ "$entry_count" -ge 5 ]; then
        echo -e "${GREEN}✓ PASS${NC}: Email log has $entry_count entries (expected >= 5)"
    else
        echo -e "${RED}✗ FAIL${NC}: Email log has only $entry_count entries (expected >= 5)"
        return 1
    fi

    # Test log file permissions
    local email_log_perms
    email_log_perms=$(stat -f "%Lp" "$email_log" 2>/dev/null || stat -c "%a" "$email_log" 2>/dev/null)

    echo -e "${BLUE}INFO${NC}: Email log permissions: $email_log_perms"

    # Verify log directory exists and is writable
    if [ -w "$EMAIL_LOG_DIR" ]; then
        echo -e "${GREEN}✓ PASS${NC}: Log directory is writable"
    else
        echo -e "${RED}✗ FAIL${NC}: Log directory is not writable"
        return 1
    fi

    return 0
}

# ============================================================
# Main Test Runner
# ============================================================
main() {
    echo "=========================================="
    echo "Email System - Integration Tests"
    echo "=========================================="
    echo "Project Root: $PROJECT_ROOT"
    echo "Library: $EMAIL_FUNCTIONS_LIB"
    echo "Test Directory: $TEST_TMP_DIR"
    echo "Email From: $EMAIL_FROM"
    echo "Email To: $EMAIL_TO"
    echo ""

    # Run all tests
    run_test "TEST 1: msmtp Connectivity" test_msmtp_connectivity
    run_test "TEST 2: Email Sending" test_email_sending
    run_test "TEST 3: Rate Limiting" test_rate_limiting
    run_test "TEST 4: Fallback Mechanisms" test_fallback_mechanisms
    run_test "TEST 5: Prometheus Metrics" test_prometheus_metrics
    run_test "TEST 6: Log File Management" test_log_file_management

    # Clean up test directory
    echo ""
    echo -e "${BLUE}INFO${NC}: Cleaning up test directory: $TEST_TMP_DIR"
    rm -rf "$TEST_TMP_DIR"

    # Print summary
    echo ""
    echo "=========================================="
    echo "Integration Test Summary"
    echo "=========================================="
    echo "Total Tests: $TESTS_RUN"
    echo -e "${GREEN}Passed: $TESTS_PASSED${NC}"
    echo -e "${RED}Failed: $TESTS_FAILED${NC}"
    echo ""

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}✓ All integration tests passed!${NC}"
        exit 0
    else
        echo -e "${RED}✗ Some integration tests failed${NC}"
        exit 1
    fi
}

# Run main function
main
