#!/usr/bin/env bash
# ============================================================
# CatchUp Feed - Disk Usage Monitoring Script
# ============================================================
# Monitors filesystem disk usage and sends email alerts when
# thresholds are exceeded.
#
# Thresholds:
#   - Warning: 75% usage → send normal priority email
#   - Critical: 85% usage → send high priority email
#   - Silent: < 75% → no email
#
# Monitored Filesystems:
#   - / (root)
#   - /var
#   - /home
#   - /var/lib/docker
#
# Features:
#   - Checks all filesystems in one run
#   - Sends ONE consolidated email for all issues
#   - Includes top 10 space consumers for problematic filesystems
#   - Provides actionable cleanup recommendations
#   - Logs all checks with correlation ID
#
# Exit Codes:
#   0: Success (check completed, with or without alerts)
#   1: Missing dependency (email-functions.sh)
#   2: Email sending failed
#
# Usage:
#   ./scripts/disk-usage-check.sh
# ============================================================

set -euo pipefail

# ============================================================
# Configuration
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
EMAIL_FUNCTIONS="${SCRIPT_DIR}/lib/email-functions.sh"

# Thresholds (percentage)
WARNING_THRESHOLD=75
CRITICAL_THRESHOLD=85

# Filesystems to monitor
FILESYSTEMS_TO_MONITOR=(
    "/"
    "/var"
    "/home"
    "/var/lib/docker"
)

# ============================================================
# Load Dependencies
# ============================================================

# Source email functions library
if [ ! -f "$EMAIL_FUNCTIONS" ]; then
    echo "ERROR: Email functions library not found: ${EMAIL_FUNCTIONS}" >&2
    exit 1
fi

source "$EMAIL_FUNCTIONS"

# ============================================================
# Utility Functions
# ============================================================

# Get disk usage percentage for a filesystem
# Arguments:
#   $1: filesystem mount point
# Returns:
#   Usage percentage (integer, e.g., 78) or empty if not found
get_disk_usage() {
    local filesystem="$1"

    # Use df to get usage percentage
    # -P: POSIX output format (ensures single line per filesystem)
    # Skip header line, match filesystem, extract 5th field (use%), remove %
    df -P "$filesystem" 2>/dev/null | awk 'NR==2 {gsub(/%/, "", $5); print $5}'
}

# Get filesystem info for display
# Arguments:
#   $1: filesystem mount point
# Returns:
#   Formatted string: "filesystem size used avail use% mounted"
get_filesystem_info() {
    local filesystem="$1"

    # Use df -h for human-readable output
    df -h -P "$filesystem" 2>/dev/null | awk 'NR==2 {print $1, $2, $3, $4, $5, $6}'
}

# Get top 10 largest directories in a filesystem
# Arguments:
#   $1: filesystem mount point
# Returns:
#   Top 10 directories with sizes
get_top_space_consumers() {
    local filesystem="$1"

    # Use du to find large directories
    # -h: human-readable
    # -d 2: depth 2 (avoid going too deep)
    # Sort by size (largest first)
    # Limit to 10 results
    # Suppress permission denied errors
    du -h -d 2 "$filesystem" 2>/dev/null | sort -rh | head -n 10
}

# ============================================================
# Main Monitoring Logic
# ============================================================

main() {
    # Generate correlation ID for this monitoring run
    local correlation_id
    correlation_id=$(generate_correlation_id)

    log_json "info" "Starting disk usage check" "$correlation_id" "{\"filesystems\":${#FILESYSTEMS_TO_MONITOR[@]}}"

    # Arrays to track problematic filesystems
    local -a critical_filesystems=()
    local -a warning_filesystems=()
    local -a all_filesystem_status=()

    # Check each filesystem
    for fs in "${FILESYSTEMS_TO_MONITOR[@]}"; do
        # Check if filesystem exists
        if ! mountpoint -q "$fs" 2>/dev/null && [ ! -d "$fs" ]; then
            log_json "debug" "Filesystem not found: $fs" "$correlation_id"
            continue
        fi

        # Get usage percentage
        local usage
        usage=$(get_disk_usage "$fs")

        # Skip if unable to determine usage
        if [ -z "$usage" ]; then
            log_json "warn" "Unable to determine disk usage for $fs" "$correlation_id"
            continue
        fi

        # Get filesystem info for display
        local fs_info
        fs_info=$(get_filesystem_info "$fs")

        # Store filesystem status
        all_filesystem_status+=("$fs_info")

        # Categorize filesystem based on usage
        if [ "$usage" -ge "$CRITICAL_THRESHOLD" ]; then
            critical_filesystems+=("$fs:$usage")
            log_json "error" "Critical disk usage on $fs" "$correlation_id" "{\"usage_percent\":$usage,\"threshold\":$CRITICAL_THRESHOLD}"
        elif [ "$usage" -ge "$WARNING_THRESHOLD" ]; then
            warning_filesystems+=("$fs:$usage")
            log_json "warn" "Warning disk usage on $fs" "$correlation_id" "{\"usage_percent\":$usage,\"threshold\":$WARNING_THRESHOLD}"
        else
            log_json "debug" "Disk usage OK on $fs" "$correlation_id" "{\"usage_percent\":$usage}"
        fi
    done

    # Check if we need to send an alert
    if [ ${#critical_filesystems[@]} -eq 0 ] && [ ${#warning_filesystems[@]} -eq 0 ]; then
        log_json "info" "Disk usage check completed - all filesystems OK" "$correlation_id"
        return 0
    fi

    # Build email alert
    local hostname
    hostname=$(hostname -s 2>/dev/null || hostname)

    local subject="⚠️ Disk Usage Alert on ${hostname}"

    # Determine priority (high if any critical, normal if only warnings)
    local priority="normal"
    if [ ${#critical_filesystems[@]} -gt 0 ]; then
        priority="high"
    fi

    # Build email body
    local body=""
    body+="⚠️ Disk Usage Alert on ${hostname}\n"
    body+="\n"

    # Critical filesystems section
    if [ ${#critical_filesystems[@]} -gt 0 ]; then
        body+="Critical Filesystems (${#critical_filesystems[@]}):\n"
        for fs_usage in "${critical_filesystems[@]}"; do
            local fs="${fs_usage%:*}"
            local usage="${fs_usage#*:}"
            local fs_info
            fs_info=$(get_filesystem_info "$fs")
            local fs_name=$(echo "$fs_info" | awk '{print $1}')
            local fs_size=$(echo "$fs_info" | awk '{print $2}')
            local fs_used=$(echo "$fs_info" | awk '{print $3}')
            body+="- ${fs}: ${usage}% used (${fs_used} / ${fs_size})\n"
        done
        body+="\n"
    fi

    # Warning filesystems section
    if [ ${#warning_filesystems[@]} -gt 0 ]; then
        body+="Warning Filesystems (${#warning_filesystems[@]}):\n"
        for fs_usage in "${warning_filesystems[@]}"; do
            local fs="${fs_usage%:*}"
            local usage="${fs_usage#*:}"
            local fs_info
            fs_info=$(get_filesystem_info "$fs")
            local fs_name=$(echo "$fs_info" | awk '{print $1}')
            local fs_size=$(echo "$fs_info" | awk '{print $2}')
            local fs_used=$(echo "$fs_info" | awk '{print $3}')
            body+="- ${fs}: ${usage}% used (${fs_used} / ${fs_size})\n"
        done
        body+="\n"
    fi

    # All monitored filesystems status
    body+="All Monitored Filesystems:\n"
    body+="$(printf '%-20s %-10s %-10s %-10s %-6s %-20s\n' 'Filesystem' 'Size' 'Used' 'Avail' 'Use%' 'Mounted on')\n"
    for fs_info in "${all_filesystem_status[@]}"; do
        # Parse filesystem info
        local fs_name=$(echo "$fs_info" | awk '{print $1}')
        local fs_size=$(echo "$fs_info" | awk '{print $2}')
        local fs_used=$(echo "$fs_info" | awk '{print $3}')
        local fs_avail=$(echo "$fs_info" | awk '{print $4}')
        local fs_use=$(echo "$fs_info" | awk '{print $5}')
        local fs_mount=$(echo "$fs_info" | awk '{print $6}')
        body+="$(printf '%-20s %-10s %-10s %-10s %-6s %-20s\n' "$fs_name" "$fs_size" "$fs_used" "$fs_avail" "$fs_use" "$fs_mount")\n"
    done
    body+="\n"

    # Top space consumers for problematic filesystems
    for fs_usage in "${critical_filesystems[@]}" "${warning_filesystems[@]}"; do
        local fs="${fs_usage%:*}"
        body+="Top Space Consumers on ${fs}:\n"
        local top_consumers
        top_consumers=$(get_top_space_consumers "$fs")
        local line_num=1
        while IFS= read -r line; do
            if [ $line_num -le 10 ]; then
                body+="$(printf '%2d. %s\n' $line_num "$line")\n"
                ((line_num++))
            fi
        done <<< "$top_consumers"
        body+="\n"
    done

    # Recommended cleanup actions
    body+="Recommended Actions:\n"
    body+="1. Clean Docker resources:\n"
    body+="   cd ~/catchup-feed\n"
    body+="   docker compose down\n"
    body+="   docker system prune -af --volumes\n"
    body+="\n"
    body+="2. Clean old logs:\n"
    body+="   sudo find /var/log -name \"*.log\" -mtime +30 -delete\n"
    body+="   sudo journalctl --vacuum-time=7d\n"
    body+="\n"
    body+="3. Clean apt cache (Debian/Ubuntu):\n"
    body+="   sudo apt clean\n"
    body+="   sudo apt autoremove\n"
    body+="\n"
    body+="4. Check largest files:\n"
    body+="   sudo du -ah /var | sort -rh | head -20\n"
    body+="\n"

    # Add timestamp and correlation ID
    local timestamp
    timestamp=$(date -Iseconds)
    body+="Timestamp: ${timestamp}\n"
    body+="Hostname: ${hostname}\n"
    body+="Correlation ID: ${correlation_id}\n"

    # Send email (convert \n to actual newlines)
    local email_body
    email_body=$(echo -e "$body")

    if send_email "$subject" "$email_body" "$correlation_id" "$priority"; then
        log_json "info" "Disk usage alert sent successfully" "$correlation_id" "{\"priority\":\"$priority\",\"critical_count\":${#critical_filesystems[@]},\"warning_count\":${#warning_filesystems[@]}}"
        return 0
    else
        log_json "error" "Failed to send disk usage alert" "$correlation_id"
        return 2
    fi
}

# ============================================================
# Execute Main
# ============================================================

main "$@"
