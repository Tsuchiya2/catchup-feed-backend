#!/usr/bin/env bash
# ============================================================
# Email Functions Library - catchup-feed notification system
# ============================================================
# Core email library providing SMTP integration, rate limiting,
# validation, and observability for the catchup-feed project.
#
# Functions:
#   - send_email: Send email via msmtp with retry logic
#   - generate_correlation_id: Generate unique correlation ID
#   - check_rate_limit: File-based rate limiting
#   - sanitize_email_content: Sanitize email content
#   - validate_email: RFC 5322 basic email validation
#   - alert_fallback: Fallback alerting via syslog
#   - check_consecutive_failures: Monitor email failures
#   - update_prometheus_metrics: Update Prometheus metrics
#
# Environment Variables:
#   EMAIL_FROM: Sender email address
#   EMAIL_TO: Recipient email address
#   EMAIL_ENABLED: Enable/disable email sending (true/false)
#   SMTP_TIMEOUT: SMTP timeout in seconds (default: 30)
#   EMAIL_RATE_LIMIT_HOURLY: Hourly rate limit (default: 10)
#   EMAIL_RATE_LIMIT_DAILY: Daily rate limit (default: 100)
#   EMAIL_LOG_DIR: Log directory (default: /var/log/catchup)
#
# Usage:
#   source scripts/lib/email-functions.sh
#   send_email "Subject" "Body" "correlation-id" "high"
# ============================================================

set -euo pipefail

# ============================================================
# Configuration and Constants
# ============================================================

# Detect script directory and project root
if [ -n "${BASH_SOURCE[0]:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
else
    # Fallback if sourced in a way that BASH_SOURCE is not set
    SCRIPT_DIR="$(pwd)"
    PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." 2>/dev/null && pwd || pwd)"
fi

# Default environment variables
EMAIL_FROM="${EMAIL_FROM:-workshop2tsuchiya.iris@gmail.com}"
EMAIL_TO="${EMAIL_TO:-workshop2tsuchiya.iris@gmail.com}"
EMAIL_ENABLED="${EMAIL_ENABLED:-true}"
SMTP_TIMEOUT="${SMTP_TIMEOUT:-30}"
EMAIL_RATE_LIMIT_HOURLY="${EMAIL_RATE_LIMIT_HOURLY:-10}"
EMAIL_RATE_LIMIT_DAILY="${EMAIL_RATE_LIMIT_DAILY:-100}"
EMAIL_LOG_DIR="${EMAIL_LOG_DIR:-/var/log/catchup}"

# Constants
readonly EMAIL_MAX_LENGTH=10000
readonly EMAIL_MAX_RETRIES=3
readonly EMAIL_RETRY_BASE_DELAY=2
readonly PROMETHEUS_METRICS_DIR="${PROMETHEUS_METRICS_DIR:-/var/lib/node_exporter/textfile_collector}"
readonly PROMETHEUS_METRICS_FILE="${PROMETHEUS_METRICS_DIR}/email_metrics.prom"

# Rate limit tracking files
readonly RATE_LIMIT_LOG="${EMAIL_LOG_DIR}/email-rate-limit.log"
readonly EMAIL_LOG="${EMAIL_LOG_DIR}/email.log"
readonly ALERT_FILE="${EMAIL_LOG_DIR}/ALERT"

# ============================================================
# Utility Functions
# ============================================================

# Escape string for JSON
# Arguments:
#   $1: string to escape
# Returns:
#   Escaped string safe for JSON
escape_json_string() {
    local str="${1:-}"
    # Escape backslashes first
    str="${str//\\/\\\\}"
    # Escape double quotes
    str="${str//\"/\\\"}"
    # Escape newlines
    str="${str//$'\n'/\\n}"
    # Escape carriage returns
    str="${str//$'\r'/\\r}"
    # Escape tabs
    str="${str//$'\t'/\\t}"
    echo "$str"
}

# Log JSON message to email.log
# Arguments:
#   $1: level (info, warn, error, debug)
#   $2: message
#   $3: correlation_id (optional)
#   $4: additional_data (optional JSON object)
log_json() {
    local level="${1:-info}"
    local message="${2:-}"
    local correlation_id="${3:-}"
    local additional_data="${4:-{}}"
    local timestamp
    timestamp="$(date -Iseconds)"
    local hostname
    hostname="$(hostname -s 2>/dev/null || hostname)"
    local pid="$$"

    # Ensure log directory exists
    mkdir -p "$EMAIL_LOG_DIR"

    # Escape special characters in message for JSON
    # Replace double quotes, backslashes, and newlines
    local escaped_message="${message//\\/\\\\}"  # Escape backslashes first
    escaped_message="${escaped_message//\"/\\\"}"  # Escape double quotes
    escaped_message="${escaped_message//$'\n'/\\n}"  # Escape newlines
    escaped_message="${escaped_message//$'\r'/\\r}"  # Escape carriage returns
    escaped_message="${escaped_message//$'\t'/\\t}"  # Escape tabs

    # Build JSON log entry
    local json_log
    json_log="{\"timestamp\":\"$timestamp\",\"level\":\"$level\",\"message\":\"$escaped_message\",\"correlation_id\":\"$correlation_id\",\"hostname\":\"$hostname\",\"pid\":$pid,\"additional_data\":$additional_data}"

    echo "$json_log" >> "$EMAIL_LOG"
}

# ============================================================
# Function: generate_correlation_id
# ============================================================
# Generate a unique correlation ID for request tracing
#
# Format: {timestamp}-{hostname}-{pid}-{random_hex}
# Example: 1700201445-raspberrypi-12345-a3f8c2d1
#
# Returns:
#   String: correlation ID
# ============================================================
generate_correlation_id() {
    local timestamp
    timestamp="$(date +%s)"
    local hostname
    hostname="$(hostname)"
    local pid="$$"
    local random_hex
    random_hex="$(openssl rand -hex 4)"

    echo "${timestamp}-${hostname}-${pid}-${random_hex}"
}

# ============================================================
# Function: sanitize_email_content
# ============================================================
# Sanitize email content by removing shell metacharacters,
# escaping newlines, and limiting length
#
# Arguments:
#   $1: content - Email content to sanitize
#
# Returns:
#   String: sanitized content
# ============================================================
sanitize_email_content() {
    local content="${1:-}"

    # Remove shell metacharacters: ; | & $ ( ) < > { } `
    content="${content//;/ }"
    content="${content//|/ }"
    content="${content//&/ }"
    content="${content//\$/ }"
    content="${content//(/ }"
    content="${content//)/ }"
    content="${content//</ }"
    content="${content//>/ }"
    content="${content//\{/ }"
    content="${content//\}/ }"
    content="${content//\`/ }"

    # Limit length to EMAIL_MAX_LENGTH characters
    if [ ${#content} -gt $EMAIL_MAX_LENGTH ]; then
        content="${content:0:$EMAIL_MAX_LENGTH}... [truncated]"
    fi

    echo "$content"
}

# ============================================================
# Function: validate_email
# ============================================================
# Validate email address using RFC 5322 basic pattern
#
# Arguments:
#   $1: email - Email address to validate
#
# Returns:
#   0: valid email
#   1: invalid email
# ============================================================
validate_email() {
    local email="${1:-}"

    # RFC 5322 basic email pattern
    local email_regex="^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$"

    if [[ "$email" =~ $email_regex ]]; then
        return 0
    else
        return 1
    fi
}

# ============================================================
# Function: check_rate_limit
# ============================================================
# Check if email sending is within rate limits
# Limits: 10/hour, 100/day (configurable via env vars)
# High priority emails bypass hourly limit
#
# Arguments:
#   $1: priority - Email priority (high/normal/low), default: normal
#
# Returns:
#   0: OK to send
#   1: rate limited
# ============================================================
check_rate_limit() {
    local priority="${1:-normal}"

    # Ensure log directory exists
    mkdir -p "$EMAIL_LOG_DIR"

    # Create rate limit log if it doesn't exist
    touch "$RATE_LIMIT_LOG"

    local current_timestamp
    current_timestamp="$(date +%s)"
    local one_hour_ago=$((current_timestamp - 3600))
    local one_day_ago=$((current_timestamp - 86400))

    # Count emails sent in the last hour
    local hourly_count=0
    if [ -f "$RATE_LIMIT_LOG" ]; then
        hourly_count=$(awk -v ts="$one_hour_ago" '$1 > ts' "$RATE_LIMIT_LOG" | wc -l | tr -d ' ')
    fi

    # Count emails sent in the last day
    local daily_count=0
    if [ -f "$RATE_LIMIT_LOG" ]; then
        daily_count=$(awk -v ts="$one_day_ago" '$1 > ts' "$RATE_LIMIT_LOG" | wc -l | tr -d ' ')
    fi

    # High priority bypasses hourly limit
    if [ "$priority" != "high" ]; then
        if [ "$hourly_count" -ge "$EMAIL_RATE_LIMIT_HOURLY" ]; then
            log_json "warn" "Hourly rate limit exceeded" "" "{\"hourly_count\":$hourly_count,\"limit\":$EMAIL_RATE_LIMIT_HOURLY}"
            return 1
        fi
    fi

    # Check daily limit (applies to all priorities)
    if [ "$daily_count" -ge "$EMAIL_RATE_LIMIT_DAILY" ]; then
        log_json "warn" "Daily rate limit exceeded" "" "{\"daily_count\":$daily_count,\"limit\":$EMAIL_RATE_LIMIT_DAILY}"
        return 1
    fi

    # Record this send attempt
    echo "$current_timestamp $priority" >> "$RATE_LIMIT_LOG"

    # Cleanup old entries (older than 1 day) to keep file size manageable
    if [ -f "$RATE_LIMIT_LOG" ]; then
        awk -v ts="$one_day_ago" '$1 > ts' "$RATE_LIMIT_LOG" > "${RATE_LIMIT_LOG}.tmp"
        mv "${RATE_LIMIT_LOG}.tmp" "$RATE_LIMIT_LOG"
    fi

    return 0
}

# ============================================================
# Function: alert_fallback
# ============================================================
# Fallback alerting mechanism when email fails
# Logs to syslog and creates ALERT file
#
# Arguments:
#   $1: severity - Alert severity (info/warning/error/critical)
#   $2: message - Alert message
#
# Returns:
#   0: always succeeds
# ============================================================
alert_fallback() {
    local severity="${1:-info}"
    local message="${2:-}"
    local timestamp
    timestamp="$(date -Iseconds)"

    # Ensure log directory exists
    mkdir -p "$EMAIL_LOG_DIR"

    # Log to syslog with mail.$severity priority
    logger -p "mail.${severity}" -t "catchup-email" "$message" 2>/dev/null || true

    # Append to ALERT file
    echo "[$timestamp] [${severity^^}] $message" >> "$ALERT_FILE"

    return 0
}

# ============================================================
# Function: check_consecutive_failures
# ============================================================
# Check last 10 email.log entries for consecutive failures
# Trigger alert_fallback if >= 3 failures detected
#
# Returns:
#   0: OK (< 3 failures)
#   1: too many failures (>= 3 failures)
# ============================================================
check_consecutive_failures() {
    # Ensure log file exists
    if [ ! -f "$EMAIL_LOG" ]; then
        return 0
    fi

    # Get last 10 log entries
    local last_entries
    last_entries=$(tail -n 10 "$EMAIL_LOG")

    # Count failures (entries with status "failure")
    local failure_count
    failure_count=$(echo "$last_entries" | grep -c '"status":"failure"' || true)

    if [ "$failure_count" -ge 3 ]; then
        alert_fallback "critical" "Email system: 3+ consecutive failures"
        return 1
    fi

    return 0
}

# ============================================================
# Function: update_prometheus_metrics
# ============================================================
# Update Prometheus textfile collector metrics
# Uses atomic write (temp file + mv) for safety
#
# Arguments:
#   $1: metric_name - Prometheus metric name
#   $2: value - Metric value
#   $3: labels - Metric labels (optional, e.g., 'status="success"')
#
# Returns:
#   0: success
#   1: failure
# ============================================================
update_prometheus_metrics() {
    local metric_name="${1:-}"
    local value="${2:-0}"
    local labels="${3:-}"
    local timestamp_ms

    timestamp_ms=$(date +%s)000

    # Validate metric name
    if [ -z "$metric_name" ]; then
        return 1
    fi

    # Create metrics directory if it doesn't exist
    if [ ! -d "$PROMETHEUS_METRICS_DIR" ]; then
        if ! mkdir -p "$PROMETHEUS_METRICS_DIR" 2>/dev/null; then
            return 1
        fi
    fi

    # Atomic write using temp file
    local temp_file="${PROMETHEUS_METRICS_FILE}.tmp.$$"

    # If metrics file exists, preserve existing metrics (excluding the one we're updating)
    if [ -f "$PROMETHEUS_METRICS_FILE" ]; then
        grep -v "^${metric_name}{" "$PROMETHEUS_METRICS_FILE" 2>/dev/null > "$temp_file" || true
    else
        : > "$temp_file"
    fi

    # Add new/updated metric with timestamp
    if [ -n "$labels" ]; then
        echo "${metric_name}{${labels}} ${value} ${timestamp_ms}" >> "$temp_file"
    else
        echo "${metric_name} ${value} ${timestamp_ms}" >> "$temp_file"
    fi

    # Atomic move
    if mv "$temp_file" "$PROMETHEUS_METRICS_FILE" 2>/dev/null; then
        chmod 644 "$PROMETHEUS_METRICS_FILE" 2>/dev/null || true
        return 0
    else
        rm -f "$temp_file" 2>/dev/null || true
        return 1
    fi
}

# ============================================================
# Function: send_email
# ============================================================
# Send email via msmtp with rate limiting, retry logic,
# and observability
#
# Arguments:
#   $1: subject - Email subject
#   $2: body - Email body
#   $3: correlation_id - Correlation ID (optional, auto-generated if not provided)
#   $4: priority - Email priority (high/normal/low, default: normal)
#
# Returns:
#   0: success
#   1: failure
#   2: rate limited
# ============================================================
send_email() {
    local subject="${1:-}"
    local body="${2:-}"
    local correlation_id="${3:-}"
    local priority="${4:-normal}"
    local start_time
    local end_time
    local latency_ms

    # Auto-generate correlation ID if not provided
    if [ -z "$correlation_id" ]; then
        correlation_id="$(generate_correlation_id)"
    fi

    # Validate inputs
    if [ -z "$subject" ]; then
        echo "{\"timestamp\":\"$(date -Iseconds)\",\"correlation_id\":\"$correlation_id\",\"event\":\"send_failed\",\"status\":\"failure\",\"reason\":\"missing_subject\"}" >> "$EMAIL_LOG"
        return 1
    fi

    # Check if email is enabled
    if [ "$EMAIL_ENABLED" != "true" ]; then
        echo "{\"timestamp\":\"$(date -Iseconds)\",\"correlation_id\":\"$correlation_id\",\"event\":\"send_skipped\",\"reason\":\"email_disabled\"}" >> "$EMAIL_LOG"
        return 1
    fi

    # Validate email addresses
    if ! validate_email "$EMAIL_FROM" || ! validate_email "$EMAIL_TO"; then
        echo "{\"timestamp\":\"$(date -Iseconds)\",\"correlation_id\":\"$correlation_id\",\"event\":\"send_failed\",\"status\":\"failure\",\"reason\":\"invalid_email\"}" >> "$EMAIL_LOG"
        alert_fallback "error" "Invalid email address configuration"
        return 1
    fi

    # Check rate limit
    if ! check_rate_limit "$priority"; then
        echo "{\"timestamp\":\"$(date -Iseconds)\",\"correlation_id\":\"$correlation_id\",\"event\":\"send_failed\",\"status\":\"rate_limited\",\"priority\":\"$priority\"}" >> "$EMAIL_LOG"
        update_prometheus_metrics "catchup_email_rate_limited_total" "1" "priority=\"$priority\""
        return 2
    fi

    # Sanitize content
    local sanitized_subject
    local sanitized_body
    sanitized_subject="$(sanitize_email_content "$subject")"
    sanitized_body="$(sanitize_email_content "$body")"

    # Retry loop with exponential backoff (1s, 2s, 4s)
    local attempt=0
    local max_attempts=3
    local backoff=1
    local send_result

    while [ $attempt -lt $max_attempts ]; do
        ((attempt++))

        start_time=$(date +%s%3N)

        # Send email via msmtp with timeout
        if timeout "$SMTP_TIMEOUT" msmtp -t <<EOF 2>/dev/null
From: ${EMAIL_FROM}
To: ${EMAIL_TO}
Subject: ${sanitized_subject}
Content-Type: text/plain; charset=UTF-8

${sanitized_body}

---
Correlation ID: ${correlation_id}
Priority: ${priority}
Timestamp: $(date -Iseconds)
EOF
        then
            end_time=$(date +%s%3N)
            latency_ms=$((end_time - start_time))

            # Log success
            echo "{\"timestamp\":\"$(date -Iseconds)\",\"correlation_id\":\"$correlation_id\",\"event\":\"send_attempt\",\"status\":\"success\",\"priority\":\"$priority\",\"subject\":\"$sanitized_subject\",\"latency_ms\":$latency_ms,\"attempt\":$attempt}" >> "$EMAIL_LOG"

            # Update rate limit tracker
            echo "$(date +%s) $correlation_id $priority" >> "$RATE_LIMIT_LOG"

            # Update Prometheus metrics
            update_prometheus_metrics "catchup_email_sent_total" "1" "status=\"success\",priority=\"$priority\""
            update_prometheus_metrics "catchup_email_latency_ms" "$latency_ms" "priority=\"$priority\""

            return 0
        else
            send_result=$?
            end_time=$(date +%s%3N)
            latency_ms=$((end_time - start_time))

            # Log failure
            echo "{\"timestamp\":\"$(date -Iseconds)\",\"correlation_id\":\"$correlation_id\",\"event\":\"send_attempt\",\"status\":\"failure\",\"priority\":\"$priority\",\"subject\":\"$sanitized_subject\",\"latency_ms\":$latency_ms,\"attempt\":$attempt,\"max_attempts\":$max_attempts}" >> "$EMAIL_LOG"

            # Check if we should retry
            if [ $attempt -lt $max_attempts ]; then
                sleep "$backoff"
                backoff=$((backoff * 2))
            fi
        fi
    done

    # All attempts failed
    update_prometheus_metrics "catchup_email_sent_total" "1" "status=\"failure\",priority=\"$priority\""
    alert_fallback "error" "Failed to send email after $max_attempts attempts: $sanitized_subject ($correlation_id)"
    check_consecutive_failures

    return 1
}

# ============================================================
# Self-test function (optional, for debugging)
# ============================================================
# Run self-test if script is executed directly
if [ -n "${BASH_SOURCE[0]:-}" ] && [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    echo "Email Functions Library - Self Test"
    echo "===================================="
    echo ""

    # Test 1: Generate correlation ID
    echo "Test 1: Generate correlation ID"
    CORR_ID=$(generate_correlation_id)
    echo "  Correlation ID: $CORR_ID"
    echo ""

    # Test 2: Validate email
    echo "Test 2: Validate email"
    if validate_email "test@example.com"; then
        echo "  ✓ test@example.com is valid"
    else
        echo "  ✗ test@example.com is invalid"
    fi

    if validate_email "invalid-email"; then
        echo "  ✗ invalid-email should be invalid"
    else
        echo "  ✓ invalid-email is correctly identified as invalid"
    fi
    echo ""

    # Test 3: Sanitize content
    echo "Test 3: Sanitize content"
    SANITIZED=$(sanitize_email_content "Hello; rm -rf /; $(whoami)")
    echo "  Original: Hello; rm -rf /; \$(whoami)"
    echo "  Sanitized: $SANITIZED"
    echo ""

    # Test 4: Check rate limit
    echo "Test 4: Check rate limit"
    if check_rate_limit "normal"; then
        echo "  ✓ Rate limit check passed"
    else
        echo "  ✗ Rate limit exceeded"
    fi
    echo ""

    echo "Self-test completed!"
    echo ""
    echo "To send a test email, run:"
    echo "  send_email \"Test Subject\" \"Test Body\" \"\" \"normal\""
fi
