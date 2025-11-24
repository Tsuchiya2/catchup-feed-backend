#!/usr/bin/env bash
# ============================================================
# PostgreSQL Database Backup Script with Email Notifications
# ============================================================
# Automated database backup with email notification support
# for success/failure tracking and monitoring.
#
# Features:
#   - Automated PostgreSQL backup via pg_dumpall
#   - gzip compression for space efficiency
#   - Retention policy (7 days default)
#   - Email notifications on success/failure
#   - Correlation ID for request tracing
#   - Disk usage monitoring
#   - Comprehensive error handling
#
# Usage:
#   ./scripts/backup.sh [OPTIONS]
#
# Options:
#   --retention DAYS  Retention days (default: 7)
#   --output DIR      Output directory (default: ~/backups)
#   --compress        Enable gzip compression (default: true)
#   --no-email        Disable email notifications
#
# Environment Variables:
#   POSTGRES_USER: PostgreSQL username (default: catchup)
#   POSTGRES_DB: PostgreSQL database name (default: catchup)
#   EMAIL_ENABLED: Enable/disable email (default: true)
#   EMAIL_FROM: Sender email address
#   EMAIL_TO: Recipient email address
#
# Log File:
#   ~/backups/backup.log (JSON format)
#
# ============================================================

set -euo pipefail

# ============================================================
# Configuration and Setup
# ============================================================

# Detect script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
LIB_DIR="$SCRIPT_DIR/lib"

# Configure email library before sourcing
# Override EMAIL_LOG_DIR to use backup directory
export EMAIL_LOG_DIR="${HOME}/backups"

# Source email functions library
if [ -f "$LIB_DIR/email-functions.sh" ]; then
    # shellcheck source=lib/email-functions.sh
    source "$LIB_DIR/email-functions.sh"
else
    echo "ERROR: email-functions.sh not found at $LIB_DIR/email-functions.sh" >&2
    exit 1
fi

# Default configuration
RETENTION_DAYS=7
OUTPUT_DIR="${HOME}/backups"
COMPRESS=true
SEND_EMAIL=true
POSTGRES_USER="${POSTGRES_USER:-catchup}"
POSTGRES_DB="${POSTGRES_DB:-catchup}"

# Log file
LOG_FILE="${OUTPUT_DIR}/backup.log"

# ============================================================
# Color Output Functions
# ============================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# ============================================================
# JSON Logging Functions
# ============================================================

# Log to JSON log file with correlation ID
# Note: Uses email-functions.sh escape_json_string for consistency
# additional_data should be passed as key-value pairs WITHOUT outer braces
# Example: "\"key1\":\"value1\",\"key2\":123"
log_backup_json() {
    local level="${1:-info}"
    local message="${2:-}"
    local correlation_id="${3:-}"
    local additional_data="${4:-}"
    local timestamp
    timestamp="$(date -Iseconds)"
    local hostname
    hostname="$(hostname -s 2>/dev/null || hostname)"
    local pid="$$"

    # Ensure output directory exists
    mkdir -p "$OUTPUT_DIR"

    # Escape message using email-functions.sh utility
    local escaped_message
    escaped_message="$(escape_json_string "$message")"

    # Build JSON log entry
    local json_log
    if [ -n "$additional_data" ]; then
        # additional_data provided, wrap it in braces
        json_log="{\"timestamp\":\"$timestamp\",\"level\":\"$level\",\"message\":\"$escaped_message\",\"correlation_id\":\"$correlation_id\",\"hostname\":\"$hostname\",\"pid\":$pid,\"additional_data\":{$additional_data}}"
    else
        # No additional_data, use empty object
        json_log="{\"timestamp\":\"$timestamp\",\"level\":\"$level\",\"message\":\"$escaped_message\",\"correlation_id\":\"$correlation_id\",\"hostname\":\"$hostname\",\"pid\":$pid,\"additional_data\":{}}"
    fi

    echo "$json_log" >> "$LOG_FILE"
}

# ============================================================
# Argument Parsing
# ============================================================

while [[ $# -gt 0 ]]; do
    case $1 in
        --retention)
            RETENTION_DAYS="$2"
            shift 2
            ;;
        --output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --compress)
            COMPRESS=true
            shift
            ;;
        --no-compress)
            COMPRESS=false
            shift
            ;;
        --no-email)
            SEND_EMAIL=false
            shift
            ;;
        *)
            log_error "Unknown option: $1"
            echo "Usage: $0 [--retention DAYS] [--output DIR] [--compress] [--no-email]"
            exit 1
            ;;
    esac
done

# ============================================================
# Main Backup Process
# ============================================================

# Generate correlation ID for this backup operation
CORRELATION_ID=$(generate_correlation_id)

# Banner
echo ""
echo "============================================================"
echo "  💾 PostgreSQL Database Backup"
echo "============================================================"
echo "  Retention: $RETENTION_DAYS days"
echo "  Output: $OUTPUT_DIR"
echo "  Compress: $COMPRESS"
echo "  Email: $SEND_EMAIL"
echo "  Correlation ID: $CORRELATION_ID"
echo "============================================================"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Log backup start
log_backup_json "info" "Database backup started" "$CORRELATION_ID" "\"retention_days\":$RETENTION_DAYS,\"compress\":\"$COMPRESS\""

# Record start time
START_TIME=$(date +%s)
START_TIME_HUMAN=$(date "+%Y-%m-%d %H:%M:%S")

# Prepare backup filename
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_FILE="db_${TIMESTAMP}.sql"

if [ "$COMPRESS" = true ]; then
    BACKUP_FILE="${BACKUP_FILE}.gz"
fi

BACKUP_PATH="$OUTPUT_DIR/$BACKUP_FILE"

# ============================================================
# Pre-flight Checks
# ============================================================

log_info "Performing pre-flight checks..."

# Check if PostgreSQL container is running
cd "$PROJECT_ROOT"
if ! docker compose ps postgres | grep -q "Up"; then
    ERROR_MSG="PostgreSQL container is not running"
    log_error "$ERROR_MSG"
    log_backup_json "error" "$ERROR_MSG" "$CORRELATION_ID"

    if [ "$SEND_EMAIL" = true ]; then
        FAILURE_EMAIL_SUBJECT="Database Backup Failed - $(date '+%Y-%m-%d %H:%M')"
        FAILURE_EMAIL_BODY="⚠️ Database Backup FAILED

Error: $ERROR_MSG

Failed Step: Pre-flight checks

Error Details:
The PostgreSQL Docker container is not running. Cannot proceed with backup.

Troubleshooting:
1. Check container status: docker compose ps postgres
2. Start PostgreSQL: docker compose up -d postgres
3. Verify container health: docker compose logs postgres
4. Check compose.yml configuration

Log File: $LOG_FILE
Hostname: $(hostname)
Correlation ID: $CORRELATION_ID"

        send_email "$FAILURE_EMAIL_SUBJECT" "$FAILURE_EMAIL_BODY" "$CORRELATION_ID" "critical" || true
    fi

    exit 1
fi

log_success "Pre-flight checks passed"

# ============================================================
# Database Backup
# ============================================================

log_info "Starting database backup..."
log_info "Backup file: $BACKUP_FILE"

BACKUP_SUCCESS=false
BACKUP_ERROR=""

if [ "$COMPRESS" = true ]; then
    # Compressed backup
    log_info "Executing: pg_dumpall | gzip > $BACKUP_PATH"
    if docker compose exec -T postgres pg_dumpall -U "$POSTGRES_USER" | gzip > "$BACKUP_PATH" 2>&1; then
        BACKUP_SUCCESS=true
        log_success "Backup completed (compressed)"
    else
        BACKUP_ERROR="pg_dumpall command failed with exit code $?"
        log_error "$BACKUP_ERROR"
        rm -f "$BACKUP_PATH"
    fi
else
    # Uncompressed backup
    log_info "Executing: pg_dumpall > $BACKUP_PATH"
    if docker compose exec -T postgres pg_dumpall -U "$POSTGRES_USER" > "$BACKUP_PATH" 2>&1; then
        BACKUP_SUCCESS=true
        log_success "Backup completed"
    else
        BACKUP_ERROR="pg_dumpall command failed with exit code $?"
        log_error "$BACKUP_ERROR"
        rm -f "$BACKUP_PATH"
    fi
fi

# Handle backup failure
if [ "$BACKUP_SUCCESS" = false ]; then
    log_backup_json "error" "Database backup failed" "$CORRELATION_ID" "\"error\":\"$BACKUP_ERROR\""

    if [ "$SEND_EMAIL" = true ]; then
        FAILURE_EMAIL_SUBJECT="Database Backup Failed - $(date '+%Y-%m-%d %H:%M')"
        FAILURE_EMAIL_BODY="⚠️ Database Backup FAILED

Error: $BACKUP_ERROR

Failed Step: Database backup

Error Details:
The pg_dumpall command failed to create a backup of the PostgreSQL database.

Troubleshooting:
1. Check PostgreSQL container status: docker compose ps postgres
2. Verify database credentials in .env file
3. Check container logs: docker compose logs postgres
4. Ensure database is accepting connections
5. Verify sufficient disk space: df -h
6. Check PostgreSQL user permissions

Log File: $LOG_FILE
Hostname: $(hostname)
Correlation ID: $CORRELATION_ID"

        send_email "$FAILURE_EMAIL_SUBJECT" "$FAILURE_EMAIL_BODY" "$CORRELATION_ID" "critical" || true
    fi

    exit 1
fi

# ============================================================
# Post-Backup Processing
# ============================================================

# Get backup file size
if [ -f "$BACKUP_PATH" ]; then
    BACKUP_SIZE=$(du -h "$BACKUP_PATH" | cut -f1)
    BACKUP_SIZE_BYTES=$(stat -f%z "$BACKUP_PATH" 2>/dev/null || stat -c%s "$BACKUP_PATH" 2>/dev/null || echo "0")
    log_info "Backup size: $BACKUP_SIZE ($BACKUP_SIZE_BYTES bytes)"
else
    log_error "Backup file not found at $BACKUP_PATH"
    exit 1
fi

# ============================================================
# Cleanup Old Backups
# ============================================================

log_info "Cleaning old backups (older than $RETENTION_DAYS days)..."
DELETED_COUNT=0
DELETED_FILES=""

while IFS= read -r -d '' file; do
    FILENAME=$(basename "$file")
    rm -f "$file"
    ((DELETED_COUNT++))
    DELETED_FILES="${DELETED_FILES}  - ${FILENAME}\n"
    log_info "Deleted: $FILENAME"
done < <(find "$OUTPUT_DIR" -name "db_*.sql*" -type f -mtime +"$RETENTION_DAYS" -print0 2>/dev/null)

if [ $DELETED_COUNT -eq 0 ]; then
    log_info "No old backups to delete"
    DELETED_FILES="  None"
else
    log_success "Deleted $DELETED_COUNT old backup(s)"
fi

# ============================================================
# Disk Usage Monitoring
# ============================================================

log_info "Checking disk usage..."
DISK_USAGE=$(df -h "$OUTPUT_DIR" | awk 'NR==2 {print $5}' | sed 's/%//')
log_info "Disk usage: ${DISK_USAGE}%"

# Warning if disk usage > 80%
if [ "$DISK_USAGE" -gt 80 ]; then
    log_warn "Disk usage is high (${DISK_USAGE}%). Consider reducing retention period or freeing up space."
fi

# ============================================================
# Calculate Duration
# ============================================================

END_TIME=$(date +%s)
END_TIME_HUMAN=$(date "+%Y-%m-%d %H:%M:%S")
DURATION=$((END_TIME - START_TIME))

log_info "Backup duration: ${DURATION} seconds"

# ============================================================
# Current Backups List
# ============================================================

echo ""
log_info "Current backups:"
BACKUP_COUNT=$(find "$OUTPUT_DIR" -name "db_*.sql*" -type f 2>/dev/null | wc -l | tr -d ' ')
ls -lht "$OUTPUT_DIR"/db_*.sql* 2>/dev/null | head -n 5 | awk '{print "  " $9 " (" $5 ")"}' || echo "  None"

if [ "$BACKUP_COUNT" -gt 5 ]; then
    echo "  ... and $((BACKUP_COUNT - 5)) more"
fi

# ============================================================
# Success Logging
# ============================================================

log_backup_json "info" "Database backup completed successfully" "$CORRELATION_ID" "\"backup_file\":\"$BACKUP_FILE\",\"size_bytes\":$BACKUP_SIZE_BYTES,\"duration_seconds\":$DURATION,\"deleted_count\":$DELETED_COUNT,\"disk_usage_percent\":$DISK_USAGE,\"retention_days\":$RETENTION_DAYS"

echo ""
log_success "Backup completed successfully!"
log_info "Backup location: $BACKUP_PATH"
echo ""

# ============================================================
# Send Success Email
# ============================================================

if [ "$SEND_EMAIL" = true ]; then
    log_info "Sending success email notification..."

    SUCCESS_EMAIL_SUBJECT="Database Backup Completed - $(date '+%Y-%m-%d %H:%M')"
    SUCCESS_EMAIL_BODY="Database Backup Completed

Backup Details:
- File: $BACKUP_FILE
- Size: $BACKUP_SIZE ($BACKUP_SIZE_BYTES bytes)
- Duration: ${DURATION} seconds
- Start time: $START_TIME_HUMAN
- End time: $END_TIME_HUMAN
- Old backups deleted: $DELETED_COUNT
- Disk usage: ${DISK_USAGE}%

Deleted Files:
$(echo -e "$DELETED_FILES")

Current Backups: $BACKUP_COUNT file(s)

Backup Location: $BACKUP_PATH

Timestamp: $END_TIME_HUMAN
Hostname: $(hostname)
Correlation ID: $CORRELATION_ID"

    if send_email "$SUCCESS_EMAIL_SUBJECT" "$SUCCESS_EMAIL_BODY" "$CORRELATION_ID" "normal"; then
        log_success "Success email sent"
    else
        log_warn "Failed to send success email (non-critical)"
    fi
fi

# ============================================================
# Restore Instructions
# ============================================================

log_info "To restore this backup, run:"
if [ "$COMPRESS" = true ]; then
    echo "  gunzip -c $BACKUP_PATH | docker compose exec -T postgres psql -U $POSTGRES_USER"
else
    echo "  docker compose exec -T postgres psql -U $POSTGRES_USER < $BACKUP_PATH"
fi
echo ""

exit 0
