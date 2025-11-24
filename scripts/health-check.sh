#!/usr/bin/env bash
# ============================================================
# CatchUp Feed - Health Check & Monitoring Script
# ============================================================
# Monitor Docker services and send email alerts on failures.
#
# Monitors:
#   1. Docker daemon status
#   2. Container health (app, worker, postgres, prometheus, grafana)
#   3. PostgreSQL connectivity
#   4. API endpoint availability
#   5. Worker process status
#
# Features:
#   - Alerts sent ONLY on failure (silent on success)
#   - Critical priority email alerts
#   - Detailed status of all services (healthy + unhealthy)
#   - Specific error details and troubleshooting steps
#   - Correlation ID for tracing
#   - JSON structured logging
#
# Exit Codes:
#   0: All services healthy (no email sent)
#   1: One or more services unhealthy (email sent)
#
# Usage:
#   ./scripts/health-check.sh
#
# Cron Example:
#   */5 * * * * /path/to/catchup-feed/scripts/health-check.sh
# ============================================================

set -euo pipefail

# ============================================================
# Configuration
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
EMAIL_FUNCTIONS="${SCRIPT_DIR}/lib/email-functions.sh"

# Environment variables (with defaults)
API_ENDPOINT="${API_ENDPOINT:-http://localhost:8080/health}"
API_TIMEOUT="${API_TIMEOUT:-5}"
EMAIL_ENABLED="${EMAIL_ENABLED:-true}"

# ============================================================
# Source Email Functions
# ============================================================

if [ ! -f "$EMAIL_FUNCTIONS" ]; then
    echo "ERROR: Email functions library not found: ${EMAIL_FUNCTIONS}" >&2
    exit 1
fi

source "$EMAIL_FUNCTIONS"

# ============================================================
# Global Variables
# ============================================================

# Generate correlation ID at start
CORRELATION_ID=$(generate_correlation_id)

# Track overall health status
OVERALL_HEALTHY=true

# Arrays to store check results
declare -a CHECK_RESULTS=()
declare -a FAILED_SERVICES=()
declare -a ERROR_DETAILS=()

# ============================================================
# Utility Functions
# ============================================================

# Log with timestamp
log_message() {
    local level="${1:-INFO}"
    local message="${2:-}"
    local timestamp
    timestamp=$(date -Iseconds)
    echo "[${timestamp}] [${level}] [${CORRELATION_ID}] ${message}"
}

# Add check result
add_check_result() {
    local service="$1"
    local status="$2"  # "healthy" or "unhealthy"
    local details="$3"

    CHECK_RESULTS+=("${status}|${service}|${details}")

    if [ "$status" = "unhealthy" ]; then
        OVERALL_HEALTHY=false
        FAILED_SERVICES+=("$service")
        ERROR_DETAILS+=("${service}: ${details}")
    fi

    log_message "INFO" "Check: ${service} - ${status} - ${details}"
}

# ============================================================
# Health Check Functions
# ============================================================

# Check 1: Docker daemon
check_docker_daemon() {
    log_message "INFO" "Checking Docker daemon..."

    if docker info >/dev/null 2>&1; then
        add_check_result "Docker Daemon" "healthy" "Running"
        return 0
    else
        add_check_result "Docker Daemon" "unhealthy" "Not responding"
        return 1
    fi
}

# Check 2: Container health status
check_containers() {
    log_message "INFO" "Checking container health..."

    # List of containers to check
    local containers=("app" "worker" "postgres" "prometheus" "grafana")

    for container in "${containers[@]}"; do
        local container_name="catchup-${container}"

        # Skip if container is postgres (different name format)
        if [ "$container" = "postgres" ]; then
            container_name="catchup-postgres"
        elif [ "$container" = "app" ]; then
            container_name="catchup-api"
        elif [ "$container" = "worker" ]; then
            container_name="catchup-worker"
        elif [ "$container" = "prometheus" ]; then
            container_name="catchup-prometheus"
        elif [ "$container" = "grafana" ]; then
            container_name="catchup-grafana"
        fi

        # Get container status
        if docker compose ps "$container" --format json 2>/dev/null | grep -q "\"State\":\"running\""; then
            # Check health status if container has healthcheck
            local health_status
            health_status=$(docker inspect --format='{{.State.Health.Status}}' "$container_name" 2>/dev/null || echo "no-healthcheck")

            # Clean up health status (remove newlines and trim)
            health_status=$(echo "$health_status" | tr -d '\n' | xargs)

            if [ "$health_status" = "healthy" ]; then
                local uptime
                uptime=$(docker inspect --format='{{.State.Status}}' "$container_name" 2>/dev/null || echo "unknown")
                add_check_result "${container} Container" "healthy" "Running (health: healthy)"
            elif [ "$health_status" = "no-healthcheck" ] || [ "$health_status" = "<no value>" ] || [ -z "$health_status" ]; then
                add_check_result "${container} Container" "healthy" "Running (no healthcheck)"
            elif [ "$health_status" = "starting" ]; then
                add_check_result "${container} Container" "healthy" "Running (health: starting)"
            else
                add_check_result "${container} Container" "unhealthy" "Running but unhealthy (health: ${health_status})"
            fi
        else
            # Container not running
            local status
            status=$(docker compose ps "$container" --format '{{.State}}' 2>/dev/null || echo "not found")
            add_check_result "${container} Container" "unhealthy" "Not running (status: ${status})"
        fi
    done
}

# Check 3: PostgreSQL connectivity
check_postgres() {
    log_message "INFO" "Checking PostgreSQL connectivity..."

    if docker compose exec -T postgres pg_isready -U "${POSTGRES_USER:-catchup}" >/dev/null 2>&1; then
        add_check_result "PostgreSQL" "healthy" "Accepting connections"
        return 0
    else
        local error_msg
        error_msg=$(docker compose exec -T postgres pg_isready -U "${POSTGRES_USER:-catchup}" 2>&1 || echo "Connection failed")
        add_check_result "PostgreSQL" "unhealthy" "Not accepting connections: ${error_msg}"
        return 1
    fi
}

# Check 4: API endpoint
check_api_endpoint() {
    log_message "INFO" "Checking API endpoint: ${API_ENDPOINT}..."

    local http_code
    local response

    # Try to get HTTP status code
    if response=$(curl -sf --max-time "$API_TIMEOUT" -w "%{http_code}" -o /dev/null "$API_ENDPOINT" 2>&1); then
        if [ "$response" = "200" ] || [ "$response" = "204" ]; then
            add_check_result "API Endpoint" "healthy" "Responding (HTTP ${response})"
            return 0
        else
            add_check_result "API Endpoint" "unhealthy" "Unexpected status code (HTTP ${response})"
            return 1
        fi
    else
        local error_msg="${response:-Connection failed}"
        add_check_result "API Endpoint" "unhealthy" "Connection failed: ${error_msg}"
        return 1
    fi
}

# Check 5: Worker process
check_worker_process() {
    log_message "INFO" "Checking worker process..."

    # Check if worker container is running
    if docker compose ps worker --format json 2>/dev/null | grep -q "\"State\":\"running\""; then
        # Get container details
        local uptime
        uptime=$(docker inspect --format='{{.State.Status}}' catchup-worker 2>/dev/null || echo "unknown")
        add_check_result "Worker Process" "healthy" "Running"
        return 0
    else
        local status
        local exit_code
        status=$(docker compose ps worker --format '{{.State}}' 2>/dev/null || echo "not found")
        exit_code=$(docker inspect --format='{{.State.ExitCode}}' catchup-worker 2>/dev/null || echo "unknown")
        add_check_result "Worker Process" "unhealthy" "Not running (status: ${status}, exit code: ${exit_code})"
        return 1
    fi
}

# ============================================================
# Email Alert Generation
# ============================================================

generate_alert_email() {
    local hostname
    hostname=$(hostname -s 2>/dev/null || hostname)
    local timestamp
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')

    # Count failed services
    local failed_count=${#FAILED_SERVICES[@]}

    # Build subject
    local subject
    subject="❌ Health Check Failed on ${hostname}"

    # Build body
    local body
    body="❌ Health Check Failed on ${hostname}

Failed Services (${failed_count}):
"

    # Add failed services
    for service in "${FAILED_SERVICES[@]}"; do
        body+="- ${service}
"
    done

    body+="
All Services Status:
"

    # Add all check results
    for result in "${CHECK_RESULTS[@]}"; do
        IFS='|' read -r status service details <<< "$result"

        if [ "$status" = "healthy" ]; then
            body+="✓ ${service}: ${details}
"
        else
            body+="✗ ${service}: ${details}
"
        fi
    done

    body+="
Error Details:
"

    # Add detailed error information
    local error_num=1
    for error in "${ERROR_DETAILS[@]}"; do
        body+="${error_num}. ${error}
"
        ((error_num++))
    done

    body+="
Troubleshooting:
1. Check container logs:
   docker compose logs --tail=50 app worker

2. Restart failed services:
   docker compose restart app worker

3. Check full service status:
   docker compose ps

4. Check Docker daemon:
   docker info

5. Check database connectivity:
   docker compose exec postgres pg_isready -U catchup

6. View API health endpoint:
   curl -v ${API_ENDPOINT}

Timestamp: ${timestamp}
Hostname: ${hostname}
Correlation ID: ${CORRELATION_ID}
"

    # Send email with critical priority
    log_message "INFO" "Sending alert email..."
    if send_email "$subject" "$body" "$CORRELATION_ID" "critical"; then
        log_message "INFO" "Alert email sent successfully"
        return 0
    else
        log_message "ERROR" "Failed to send alert email"
        return 1
    fi
}

# ============================================================
# Main Execution
# ============================================================

main() {
    log_message "INFO" "Starting health check..."
    log_message "INFO" "Correlation ID: ${CORRELATION_ID}"

    # Run all health checks (don't exit early on failure)
    check_docker_daemon || true
    check_containers || true
    check_postgres || true
    check_api_endpoint || true
    check_worker_process || true

    # Evaluate overall health
    if [ "$OVERALL_HEALTHY" = true ]; then
        log_message "INFO" "All services healthy"
        log_message "INFO" "Health check completed successfully (no alert sent)"
        exit 0
    else
        log_message "WARN" "Health check failed: ${#FAILED_SERVICES[@]} service(s) unhealthy"

        # Send alert email only on failure
        if [ "$EMAIL_ENABLED" = "true" ]; then
            generate_alert_email
        else
            log_message "WARN" "Email alerts disabled (EMAIL_ENABLED=${EMAIL_ENABLED})"
        fi

        log_message "ERROR" "Health check completed with failures"
        exit 1
    fi
}

# Run main function
main
