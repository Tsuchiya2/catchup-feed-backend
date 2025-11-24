#!/usr/bin/env bash
# ============================================================
# Prometheus Data Monitoring Script - catchup-feed
# ============================================================
# Monitor Prometheus data size and send warnings when
# thresholds are exceeded.
#
# Thresholds:
#   - Warning: 2GB → send normal priority email
#   - Critical: 5GB → send high priority email
#   - Silent: < 2GB → no email
#
# Environment Variables:
#   EMAIL_FROM: Sender email address
#   EMAIL_TO: Recipient email address
#   EMAIL_ENABLED: Enable/disable email sending (true/false)
#   COMPOSE_FILE: Path to compose.yml (default: ./compose.yml)
#   PROJECT_ROOT: Project root directory
#
# Usage:
#   ./scripts/cleanup-prometheus.sh
#
# Cron example:
#   0 */6 * * * /home/user/catchup-feed/scripts/cleanup-prometheus.sh
# ============================================================

set -euo pipefail

# ============================================================
# Configuration and Constants
# ============================================================

# Detect script directory and project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="${PROJECT_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"

# Load email functions library
# shellcheck source=scripts/lib/email-functions.sh
if [ -f "$SCRIPT_DIR/lib/email-functions.sh" ]; then
    source "$SCRIPT_DIR/lib/email-functions.sh"
else
    echo "Error: email-functions.sh not found at $SCRIPT_DIR/lib/email-functions.sh" >&2
    exit 1
fi

# Constants
readonly WARNING_THRESHOLD_GB=2
readonly CRITICAL_THRESHOLD_GB=5
readonly COMPOSE_FILE="${COMPOSE_FILE:-$PROJECT_ROOT/compose.yml}"
readonly VOLUME_NAME="catchup-feed_prometheus-data"

# Generate correlation ID for this check
CORRELATION_ID=$(generate_correlation_id)

# ============================================================
# Helper Functions
# ============================================================

# Convert bytes to GB with 2 decimal places
# Arguments:
#   $1: bytes
# Returns:
#   String: size in GB (e.g., "2.35")
bytes_to_gb() {
    local bytes="${1:-0}"
    # Use awk for floating point division
    awk "BEGIN {printf \"%.2f\", $bytes / 1024 / 1024 / 1024}"
}

# Get Prometheus data directory path
# Returns:
#   String: absolute path to data directory
#   Exit code: 0 on success, 1 on failure
get_prometheus_data_dir() {
    # Check if Docker is running
    if ! docker info >/dev/null 2>&1; then
        echo "Error: Docker is not running" >&2
        return 1
    fi

    # Check if volume exists
    if ! docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
        echo "Error: Volume $VOLUME_NAME not found" >&2
        return 1
    fi

    # Extract mount point from volume inspect
    local mount_point
    mount_point=$(docker volume inspect "$VOLUME_NAME" --format '{{.Mountpoint}}' 2>/dev/null)

    if [ -z "$mount_point" ] || [ ! -d "$mount_point" ]; then
        echo "Error: Mount point not found or not accessible: $mount_point" >&2
        return 1
    fi

    echo "$mount_point"
}

# Calculate directory size in bytes
# Arguments:
#   $1: directory path
# Returns:
#   Integer: size in bytes
calculate_directory_size() {
    local dir_path="${1:-}"

    if [ ! -d "$dir_path" ]; then
        echo "0"
        return 0
    fi

    # Use du to calculate size in bytes
    # -s: summarize (total only)
    # -b: bytes
    local size_bytes
    size_bytes=$(du -sb "$dir_path" 2>/dev/null | cut -f1)

    echo "${size_bytes:-0}"
}

# Get oldest TSDB block timestamp
# Arguments:
#   $1: data directory path
# Returns:
#   String: oldest timestamp (Unix milliseconds) or "N/A"
get_oldest_block_timestamp() {
    local data_dir="${1:-}"

    if [ ! -d "$data_dir" ]; then
        echo "N/A"
        return 0
    fi

    # Find all meta.json files and extract minTime
    local oldest_time=""

    # Use find to locate all meta.json files
    while IFS= read -r meta_file; do
        if [ -f "$meta_file" ]; then
            # Extract minTime using grep and sed (jq may not be available)
            local min_time
            min_time=$(grep -o '"minTime":[0-9]*' "$meta_file" 2>/dev/null | sed 's/"minTime"://' || echo "")

            if [ -n "$min_time" ]; then
                if [ -z "$oldest_time" ] || [ "$min_time" -lt "$oldest_time" ]; then
                    oldest_time="$min_time"
                fi
            fi
        fi
    done < <(find "$data_dir" -name "meta.json" 2>/dev/null)

    if [ -n "$oldest_time" ]; then
        # Convert milliseconds to seconds and format as ISO date
        local timestamp_sec=$((oldest_time / 1000))
        date -r "$timestamp_sec" "+%Y-%m-%d %H:%M:%S" 2>/dev/null || echo "$oldest_time"
    else
        echo "N/A"
    fi
}

# Calculate age of oldest data in days
# Arguments:
#   $1: oldest timestamp (Unix milliseconds) or "N/A"
# Returns:
#   String: age in days or "N/A"
calculate_data_age() {
    local oldest_timestamp="${1:-N/A}"

    if [ "$oldest_timestamp" = "N/A" ]; then
        echo "N/A"
        return 0
    fi

    # Try to parse the timestamp
    local timestamp_sec
    if [[ "$oldest_timestamp" =~ ^[0-9]+$ ]]; then
        # Unix milliseconds format
        timestamp_sec=$((oldest_timestamp / 1000))
    else
        # Try to parse ISO date format
        timestamp_sec=$(date -j -f "%Y-%m-%d %H:%M:%S" "$oldest_timestamp" "+%s" 2>/dev/null || echo "0")
    fi

    if [ "$timestamp_sec" = "0" ]; then
        echo "N/A"
        return 0
    fi

    local current_time
    current_time=$(date +%s)
    local age_seconds=$((current_time - timestamp_sec))
    local age_days=$((age_seconds / 86400))

    echo "$age_days"
}

# Get available disk space in GB
# Arguments:
#   $1: directory path
# Returns:
#   String: available space in GB and percentage
get_available_disk_space() {
    local dir_path="${1:-}"

    if [ ! -d "$dir_path" ]; then
        echo "N/A"
        return 0
    fi

    # Use df to get disk space information
    # -h: human readable
    local df_output
    df_output=$(df -h "$dir_path" 2>/dev/null | tail -n 1)

    # Extract available space and percentage
    local available
    local used_percent
    available=$(echo "$df_output" | awk '{print $4}')
    used_percent=$(echo "$df_output" | awk '{print $5}' | tr -d '%')

    # Calculate free percentage
    local free_percent=$((100 - used_percent))

    echo "$available ($free_percent% free)"
}

# Get current retention policy from compose.yml
# Returns:
#   String: retention time (e.g., "30d") or "N/A"
get_retention_policy() {
    if [ ! -f "$COMPOSE_FILE" ]; then
        echo "N/A"
        return 0
    fi

    # Extract retention time from compose.yml
    # Look for: '--storage.tsdb.retention.time=30d'
    local retention
    retention=$(grep -A 10 "prometheus:" "$COMPOSE_FILE" | grep "storage.tsdb.retention.time" | sed -E "s/.*'--storage.tsdb.retention.time=([^']+)'.*/\1/" || echo "N/A")

    echo "$retention"
}

# Build email body for warning/critical alerts
# Arguments:
#   $1: alert level (warning/critical)
#   $2: current size in GB
#   $3: oldest timestamp
#   $4: data age in days
#   $5: available disk space
#   $6: retention policy
# Returns:
#   String: formatted email body
build_email_body() {
    local alert_level="${1:-warning}"
    local current_size_gb="${2:-0.00}"
    local oldest_timestamp="${3:-N/A}"
    local data_age="${4:-N/A}"
    local available_space="${5:-N/A}"
    local retention_policy="${6:-N/A}"
    local hostname
    hostname="$(hostname -s 2>/dev/null || hostname)"

    local alert_icon
    if [ "$alert_level" = "critical" ]; then
        alert_icon="🔴"
    else
        alert_icon="⚠️"
    fi

    cat <<EOF
${alert_icon} Prometheus Data Size ${alert_level^^}

Current Status:
- Prometheus data size: ${current_size_gb} GB
- Warning threshold: ${WARNING_THRESHOLD_GB}.0 GB
- Critical threshold: ${CRITICAL_THRESHOLD_GB}.0 GB
- Oldest data: ${oldest_timestamp} (${data_age} days old)
- Available disk space: ${available_space}

Current Configuration:
- Retention policy: ${retention_policy}
- Volume name: ${VOLUME_NAME}
- Data directory: $(get_prometheus_data_dir 2>/dev/null || echo "N/A")

Recommended Actions:
1. Reduce retention period in compose.yml:

   prometheus:
     command:
       - '--storage.tsdb.retention.time=15d'  # Currently: ${retention_policy}

2. Restart Prometheus to apply changes:
   cd ${PROJECT_ROOT}
   docker compose restart prometheus

3. Manually delete old data (if urgent):
   docker compose exec prometheus \\
     promtool tsdb delete-blocks --retention.time=15d /prometheus

4. Verify cleanup:
   docker volume inspect ${VOLUME_NAME}
   du -sh \$(docker volume inspect ${VOLUME_NAME} --format '{{.Mountpoint}}')

Timestamp: $(date "+%Y-%m-%d %H:%M:%S")
Hostname: ${hostname}
Correlation ID: ${CORRELATION_ID}
EOF
}

# ============================================================
# Main Monitoring Logic
# ============================================================

main() {
    log_json "info" "Starting Prometheus data monitoring" "$CORRELATION_ID" "{}"

    # Get Prometheus data directory
    local data_dir
    if ! data_dir=$(get_prometheus_data_dir); then
        log_json "error" "Failed to get Prometheus data directory" "$CORRELATION_ID" "{}"
        alert_fallback "error" "Prometheus data monitoring failed: Cannot access data directory ($CORRELATION_ID)"
        exit 1
    fi

    log_json "debug" "Found Prometheus data directory: $data_dir" "$CORRELATION_ID" "{}"

    # Calculate directory size
    local size_bytes
    size_bytes=$(calculate_directory_size "$data_dir")
    local size_gb
    size_gb=$(bytes_to_gb "$size_bytes")

    log_json "info" "Prometheus data size calculated" "$CORRELATION_ID" "{\"size_bytes\":$size_bytes,\"size_gb\":$size_gb}"

    # Get additional information
    local oldest_timestamp
    oldest_timestamp=$(get_oldest_block_timestamp "$data_dir")

    local data_age
    data_age=$(calculate_data_age "$oldest_timestamp")

    local available_space
    available_space=$(get_available_disk_space "$data_dir")

    local retention_policy
    retention_policy=$(get_retention_policy)

    log_json "debug" "Collected metadata" "$CORRELATION_ID" "{\"oldest\":\"$oldest_timestamp\",\"age_days\":\"$data_age\",\"available\":\"$available_space\",\"retention\":\"$retention_policy\"}"

    # Compare with thresholds
    local threshold_exceeded="none"
    local priority="normal"
    local subject=""
    local body=""

    # Use awk for floating point comparison
    if awk "BEGIN {exit !($size_gb >= $CRITICAL_THRESHOLD_GB)}"; then
        threshold_exceeded="critical"
        priority="high"
        subject="🔴 Prometheus Data Size CRITICAL - ${size_gb} GB"
        body=$(build_email_body "critical" "$size_gb" "$oldest_timestamp" "$data_age" "$available_space" "$retention_policy")
    elif awk "BEGIN {exit !($size_gb >= $WARNING_THRESHOLD_GB)}"; then
        threshold_exceeded="warning"
        priority="normal"
        subject="⚠️ Prometheus Data Size Warning - ${size_gb} GB"
        body=$(build_email_body "warning" "$size_gb" "$oldest_timestamp" "$data_age" "$available_space" "$retention_policy")
    fi

    # Send email if threshold exceeded
    if [ "$threshold_exceeded" != "none" ]; then
        log_json "warn" "Prometheus data size threshold exceeded" "$CORRELATION_ID" "{\"level\":\"$threshold_exceeded\",\"size_gb\":$size_gb,\"threshold_gb\":$WARNING_THRESHOLD_GB}"

        if send_email "$subject" "$body" "$CORRELATION_ID" "$priority"; then
            log_json "info" "Alert email sent successfully" "$CORRELATION_ID" "{\"level\":\"$threshold_exceeded\",\"priority\":\"$priority\"}"
        else
            local send_result=$?
            log_json "error" "Failed to send alert email" "$CORRELATION_ID" "{\"level\":\"$threshold_exceeded\",\"exit_code\":$send_result}"
        fi
    else
        log_json "info" "Prometheus data size within normal range" "$CORRELATION_ID" "{\"size_gb\":$size_gb,\"warning_threshold\":$WARNING_THRESHOLD_GB}"
    fi

    # Update Prometheus metrics
    update_prometheus_metrics "catchup_prometheus_data_size_bytes" "$size_bytes" ""
    update_prometheus_metrics "catchup_prometheus_data_size_gb" "${size_gb/./}" ""  # Remove decimal for integer metric

    log_json "info" "Prometheus data monitoring completed" "$CORRELATION_ID" "{\"status\":\"success\",\"threshold_exceeded\":\"$threshold_exceeded\"}"

    return 0
}

# ============================================================
# Script Entry Point
# ============================================================

# Run main function
main "$@"
