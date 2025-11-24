#!/bin/bash
# ============================================================
# Docker Cleanup Script - Automated maintenance and reporting
# ============================================================
# Cleans up Docker resources and sends email summary report
#
# Usage:
#   ./scripts/docker-cleanup.sh
#
# Features:
#   - Removes unused images (7+ days old)
#   - Removes unused volumes (preserves labeled volumes)
#   - Cleans build cache (7+ days old)
#   - System prune (stopped containers, networks)
#   - Sends email report (always)
#   - Collects before/after statistics
# ============================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Load email functions
# shellcheck source=lib/email-functions.sh
if [ -f "$SCRIPT_DIR/lib/email-functions.sh" ]; then
    source "$SCRIPT_DIR/lib/email-functions.sh"
else
    echo "ERROR: email-functions.sh not found!"
    exit 1
fi

# Color output
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
# Parse Docker system df output
# ============================================================
# Parse docker system df output and extract statistics
#
# Arguments:
#   $1: docker system df output
#
# Returns:
#   Sets global variables: IMAGES_COUNT, IMAGES_SIZE, etc.
# ============================================================
parse_docker_df() {
    local df_output="$1"

    # Parse Images line
    # Expected format: Images          23        5         4.8GB     1.2GB (33%)
    local images_line
    images_line=$(echo "$df_output" | grep "^Images")

    if [ -n "$images_line" ]; then
        IMAGES_COUNT=$(echo "$images_line" | awk '{print $2}')
        IMAGES_ACTIVE=$(echo "$images_line" | awk '{print $3}')
        IMAGES_SIZE=$(echo "$images_line" | awk '{print $4}')
        IMAGES_RECLAIMABLE=$(echo "$images_line" | awk '{print $5}')
    else
        IMAGES_COUNT=0
        IMAGES_ACTIVE=0
        IMAGES_SIZE="0B"
        IMAGES_RECLAIMABLE="0B"
    fi

    # Parse Containers line
    local containers_line
    containers_line=$(echo "$df_output" | grep "^Containers")

    if [ -n "$containers_line" ]; then
        CONTAINERS_COUNT=$(echo "$containers_line" | awk '{print $2}')
        CONTAINERS_ACTIVE=$(echo "$containers_line" | awk '{print $3}')
        CONTAINERS_SIZE=$(echo "$containers_line" | awk '{print $4}')
        CONTAINERS_RECLAIMABLE=$(echo "$containers_line" | awk '{print $5}')
    else
        CONTAINERS_COUNT=0
        CONTAINERS_ACTIVE=0
        CONTAINERS_SIZE="0B"
        CONTAINERS_RECLAIMABLE="0B"
    fi

    # Parse Local Volumes line
    local volumes_line
    volumes_line=$(echo "$df_output" | grep "^Local Volumes")

    if [ -n "$volumes_line" ]; then
        VOLUMES_COUNT=$(echo "$volumes_line" | awk '{print $3}')
        VOLUMES_ACTIVE=$(echo "$volumes_line" | awk '{print $4}')
        VOLUMES_SIZE=$(echo "$volumes_line" | awk '{print $5}')
        VOLUMES_RECLAIMABLE=$(echo "$volumes_line" | awk '{print $6}')
    else
        VOLUMES_COUNT=0
        VOLUMES_ACTIVE=0
        VOLUMES_SIZE="0B"
        VOLUMES_RECLAIMABLE="0B"
    fi

    # Parse Build Cache line
    local cache_line
    cache_line=$(echo "$df_output" | grep "^Build Cache")

    if [ -n "$cache_line" ]; then
        CACHE_COUNT=$(echo "$cache_line" | awk '{print $3}')
        CACHE_ACTIVE=$(echo "$cache_line" | awk '{print $4}')
        CACHE_SIZE=$(echo "$cache_line" | awk '{print $5}')
        CACHE_RECLAIMABLE=$(echo "$cache_line" | awk '{print $6}')
    else
        CACHE_COUNT=0
        CACHE_ACTIVE=0
        CACHE_SIZE="0B"
        CACHE_RECLAIMABLE="0B"
    fi
}

# ============================================================
# Convert size to bytes for calculation
# ============================================================
# Convert human-readable size (e.g., "1.2GB") to bytes
#
# Arguments:
#   $1: size string (e.g., "1.2GB", "847MB", "0B")
#
# Returns:
#   Size in bytes
# ============================================================
size_to_bytes() {
    local size="$1"

    # Remove parentheses and percentages
    size=$(echo "$size" | sed 's/(.*//' | tr -d ' ')

    # Handle "0B" case
    if [ "$size" = "0B" ] || [ "$size" = "0" ]; then
        echo "0"
        return
    fi

    # Extract number and unit
    local number
    local unit
    number=$(echo "$size" | grep -oE '^[0-9.]+')
    unit=$(echo "$size" | grep -oE '[A-Za-z]+$')

    # Convert to bytes
    case "$unit" in
        B|b)
            echo "$number" | awk '{printf "%.0f", $1}'
            ;;
        KB|Kb|kb|kB)
            echo "$number" | awk '{printf "%.0f", $1 * 1024}'
            ;;
        MB|Mb|mb|mB)
            echo "$number" | awk '{printf "%.0f", $1 * 1024 * 1024}'
            ;;
        GB|Gb|gb|gB)
            echo "$number" | awk '{printf "%.0f", $1 * 1024 * 1024 * 1024}'
            ;;
        TB|Tb|tb|tB)
            echo "$number" | awk '{printf "%.0f", $1 * 1024 * 1024 * 1024 * 1024}'
            ;;
        *)
            echo "0"
            ;;
    esac
}

# ============================================================
# Convert bytes to human-readable size
# ============================================================
# Convert bytes to human-readable size (e.g., "1.2 GB")
#
# Arguments:
#   $1: size in bytes
#
# Returns:
#   Human-readable size
# ============================================================
bytes_to_human() {
    local bytes="$1"

    if [ "$bytes" -lt 1024 ]; then
        echo "${bytes} B"
    elif [ "$bytes" -lt 1048576 ]; then
        echo "$bytes" | awk '{printf "%.1f KB", $1/1024}'
    elif [ "$bytes" -lt 1073741824 ]; then
        echo "$bytes" | awk '{printf "%.1f MB", $1/1024/1024}'
    else
        echo "$bytes" | awk '{printf "%.1f GB", $1/1024/1024/1024}'
    fi
}

# ============================================================
# Main cleanup process
# ============================================================

echo ""
echo "============================================================"
echo "  🧹 Docker Cleanup - Automated Maintenance"
echo "============================================================"
echo ""

# Generate correlation ID
CORRELATION_ID=$(generate_correlation_id)
HOSTNAME=$(hostname)
TIMESTAMP=$(date +"%Y-%m-%d %H:%M:%S")

log_info "Correlation ID: $CORRELATION_ID"
log_info "Starting Docker cleanup process..."
echo ""

# ============================================================
# 1. Collect BEFORE statistics
# ============================================================
log_info "Collecting statistics before cleanup..."

BEFORE_DF=$(docker system df 2>&1)
BEFORE_DF_VERBOSE=$(docker system df -v 2>&1 || true)

# Parse before statistics
parse_docker_df "$BEFORE_DF"

BEFORE_IMAGES_COUNT=$IMAGES_COUNT
BEFORE_IMAGES_SIZE=$IMAGES_SIZE
BEFORE_CONTAINERS_COUNT=$CONTAINERS_COUNT
BEFORE_CONTAINERS_SIZE=$CONTAINERS_SIZE
BEFORE_VOLUMES_COUNT=$VOLUMES_COUNT
BEFORE_VOLUMES_SIZE=$VOLUMES_SIZE
BEFORE_CACHE_SIZE=$CACHE_SIZE

log_success "Before cleanup statistics collected"
echo "  Images: $BEFORE_IMAGES_COUNT (total size: $BEFORE_IMAGES_SIZE)"
echo "  Containers: $BEFORE_CONTAINERS_COUNT (total size: $BEFORE_CONTAINERS_SIZE)"
echo "  Volumes: $BEFORE_VOLUMES_COUNT (total size: $BEFORE_VOLUMES_SIZE)"
echo "  Build Cache: $BEFORE_CACHE_SIZE"
echo ""

# ============================================================
# 2. Cleanup Operations
# ============================================================
log_info "Starting cleanup operations..."
echo ""

# 2.1 Remove unused images (7+ days old)
log_info "Removing unused images (7+ days old)..."
IMAGES_OUTPUT=$(docker image prune -af --filter "until=168h" 2>&1 || echo "No images to remove")
IMAGES_REMOVED=$(echo "$IMAGES_OUTPUT" | grep -oE 'deleted: [0-9]+' | grep -oE '[0-9]+' || echo "0")
IMAGES_SPACE=$(echo "$IMAGES_OUTPUT" | grep -oE 'reclaimed: [0-9.]+[KMGT]?B' | grep -oE '[0-9.]+[KMGT]?B' || echo "0B")
log_success "Images removed: $IMAGES_REMOVED (reclaimed: $IMAGES_SPACE)"
echo ""

# 2.2 Remove unused volumes
log_info "Removing unused volumes..."
VOLUMES_OUTPUT=$(docker volume prune -f 2>&1 || echo "No volumes to remove")
VOLUMES_REMOVED=$(echo "$VOLUMES_OUTPUT" | grep -oE 'deleted: [0-9]+' | grep -oE '[0-9]+' || echo "0")
VOLUMES_SPACE=$(echo "$VOLUMES_OUTPUT" | grep -oE 'reclaimed: [0-9.]+[KMGT]?B' | grep -oE '[0-9.]+[KMGT]?B' || echo "0B")
log_success "Volumes removed: $VOLUMES_REMOVED (reclaimed: $VOLUMES_SPACE)"
echo ""

# 2.3 Clean build cache (7+ days old)
log_info "Cleaning build cache (7+ days old)..."
# Check if buildx is available
if docker buildx version >/dev/null 2>&1; then
    CACHE_OUTPUT=$(docker buildx prune -af --filter "until=168h" 2>&1 || echo "No cache to remove")
else
    # Fallback to builder prune if buildx is not available
    CACHE_OUTPUT=$(docker builder prune -af --filter "until=168h" 2>&1 || echo "No cache to remove")
fi
CACHE_SPACE=$(echo "$CACHE_OUTPUT" | grep -oE '[0-9.]+[KMGT]?B' | tail -1 || echo "0B")
log_success "Build cache freed: $CACHE_SPACE"
echo ""

# 2.4 System prune (stopped containers, networks)
log_info "Running system prune (stopped containers, networks)..."
SYSTEM_OUTPUT=$(docker system prune -f 2>&1 || echo "Nothing to prune")
SYSTEM_SPACE=$(echo "$SYSTEM_OUTPUT" | grep -oE 'reclaimed: [0-9.]+[KMGT]?B' | grep -oE '[0-9.]+[KMGT]?B' || echo "0B")
log_success "System prune completed: $SYSTEM_SPACE reclaimed"
echo ""

# ============================================================
# 3. Collect AFTER statistics
# ============================================================
log_info "Collecting statistics after cleanup..."

AFTER_DF=$(docker system df 2>&1)

# Parse after statistics
parse_docker_df "$AFTER_DF"

AFTER_IMAGES_COUNT=$IMAGES_COUNT
AFTER_IMAGES_SIZE=$IMAGES_SIZE
AFTER_IMAGES_RECLAIMABLE=$IMAGES_RECLAIMABLE
AFTER_CONTAINERS_COUNT=$CONTAINERS_COUNT
AFTER_CONTAINERS_SIZE=$CONTAINERS_SIZE
AFTER_CONTAINERS_RECLAIMABLE=$CONTAINERS_RECLAIMABLE
AFTER_VOLUMES_COUNT=$VOLUMES_COUNT
AFTER_VOLUMES_SIZE=$VOLUMES_SIZE
AFTER_VOLUMES_RECLAIMABLE=$VOLUMES_RECLAIMABLE
AFTER_CACHE_SIZE=$CACHE_SIZE
AFTER_CACHE_RECLAIMABLE=$CACHE_RECLAIMABLE

log_success "After cleanup statistics collected"
echo "  Images: $AFTER_IMAGES_COUNT (total size: $AFTER_IMAGES_SIZE)"
echo "  Containers: $AFTER_CONTAINERS_COUNT (total size: $AFTER_CONTAINERS_SIZE)"
echo "  Volumes: $AFTER_VOLUMES_COUNT (total size: $AFTER_VOLUMES_SIZE)"
echo "  Build Cache: $AFTER_CACHE_SIZE"
echo ""

# ============================================================
# 4. Calculate space reclaimed
# ============================================================
log_info "Calculating total space reclaimed..."

# Convert sizes to bytes for calculation
BEFORE_TOTAL_BYTES=0
BEFORE_TOTAL_BYTES=$((BEFORE_TOTAL_BYTES + $(size_to_bytes "$BEFORE_IMAGES_SIZE")))
BEFORE_TOTAL_BYTES=$((BEFORE_TOTAL_BYTES + $(size_to_bytes "$BEFORE_CONTAINERS_SIZE")))
BEFORE_TOTAL_BYTES=$((BEFORE_TOTAL_BYTES + $(size_to_bytes "$BEFORE_VOLUMES_SIZE")))
BEFORE_TOTAL_BYTES=$((BEFORE_TOTAL_BYTES + $(size_to_bytes "$BEFORE_CACHE_SIZE")))

AFTER_TOTAL_BYTES=0
AFTER_TOTAL_BYTES=$((AFTER_TOTAL_BYTES + $(size_to_bytes "$AFTER_IMAGES_SIZE")))
AFTER_TOTAL_BYTES=$((AFTER_TOTAL_BYTES + $(size_to_bytes "$AFTER_CONTAINERS_SIZE")))
AFTER_TOTAL_BYTES=$((AFTER_TOTAL_BYTES + $(size_to_bytes "$AFTER_VOLUMES_SIZE")))
AFTER_TOTAL_BYTES=$((AFTER_TOTAL_BYTES + $(size_to_bytes "$AFTER_CACHE_SIZE")))

SPACE_RECLAIMED_BYTES=$((BEFORE_TOTAL_BYTES - AFTER_TOTAL_BYTES))

# Handle negative values (shouldn't happen, but just in case)
if [ "$SPACE_RECLAIMED_BYTES" -lt 0 ]; then
    SPACE_RECLAIMED_BYTES=0
fi

SPACE_RECLAIMED_HUMAN=$(bytes_to_human "$SPACE_RECLAIMED_BYTES")

log_success "Total space reclaimed: $SPACE_RECLAIMED_HUMAN"
echo ""

# ============================================================
# 5. Generate and send email report
# ============================================================
log_info "Generating email report..."

EMAIL_SUBJECT="Docker Cleanup Report - ${HOSTNAME} - $(date +"%Y-%m-%d")"

# Build email body
EMAIL_BODY="Docker Cleanup Report

Summary:
- Space reclaimed: $SPACE_RECLAIMED_HUMAN
- Images removed: $IMAGES_REMOVED
- Volumes removed: $VOLUMES_REMOVED
- Build cache freed: $CACHE_SPACE

Before Cleanup:
- Images: $BEFORE_IMAGES_COUNT (total size: $BEFORE_IMAGES_SIZE)
- Containers: $BEFORE_CONTAINERS_COUNT (total size: $BEFORE_CONTAINERS_SIZE)
- Local Volumes: $BEFORE_VOLUMES_COUNT (total size: $BEFORE_VOLUMES_SIZE)
- Build Cache: $BEFORE_CACHE_SIZE

After Cleanup:
- Images: $AFTER_IMAGES_COUNT (total size: $AFTER_IMAGES_SIZE)
- Containers: $AFTER_CONTAINERS_COUNT (total size: $AFTER_CONTAINERS_SIZE)
- Local Volumes: $AFTER_VOLUMES_COUNT (total size: $AFTER_VOLUMES_SIZE)
- Build Cache: $AFTER_CACHE_SIZE

Current Disk Usage:
$AFTER_DF

Timestamp: $TIMESTAMP
Hostname: $HOSTNAME
Correlation ID: $CORRELATION_ID"

# Send email (always, even if nothing cleaned)
log_info "Sending email report..."

if send_email "$EMAIL_SUBJECT" "$EMAIL_BODY" "$CORRELATION_ID" "normal"; then
    log_success "Email report sent successfully"
else
    log_warn "Failed to send email report (check email logs)"
fi

echo ""
echo "============================================================"
log_success "Docker cleanup completed!"
echo "============================================================"
echo ""
echo "Summary:"
echo "  Space reclaimed: $SPACE_RECLAIMED_HUMAN"
echo "  Images removed: $IMAGES_REMOVED"
echo "  Volumes removed: $VOLUMES_REMOVED"
echo "  Build cache freed: $CACHE_SPACE"
echo ""
echo "  Correlation ID: $CORRELATION_ID"
echo ""
