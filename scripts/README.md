# CatchUp Feed - Scripts Documentation

This directory contains automation scripts for deployment, monitoring, and maintenance of the CatchUp Feed application.

---

## Table of Contents

1. [Script Overview](#script-overview)
2. [Email Notification System](#email-notification-system)
3. [Database Management](#database-management)
4. [Monitoring Scripts](#monitoring-scripts)
5. [Maintenance Scripts](#maintenance-scripts)
6. [Deployment Scripts](#deployment-scripts)
7. [Environment Variables](#environment-variables)
8. [Cron Schedule Recommendations](#cron-schedule-recommendations)
9. [Correlation ID Usage](#correlation-id-usage)

---

## Script Overview

```
scripts/
├── lib/
│   └── email-functions.sh          # Email notification library (shared)
├── backup.sh                        # Database backup with email notifications
├── backup-db.sh                     # Simple database backup (no email)
├── restore-db.sh                    # Database restore utility
├── health-check.sh                  # Service health monitoring
├── disk-usage-check.sh              # Disk space monitoring
├── cleanup-prometheus.sh            # Prometheus data cleanup
├── docker-cleanup.sh                # Docker resource cleanup
├── setup-email.sh                   # Email system setup and verification
└── build-multiarch.sh               # Multi-architecture Docker image builder
```

### Script Categories

| Category | Scripts | Purpose |
|----------|---------|---------|
| **Email System** | `setup-email.sh`, `lib/email-functions.sh` | Email notification infrastructure |
| **Database** | `backup.sh`, `backup-db.sh`, `restore-db.sh` | Database backup and restore |
| **Monitoring** | `health-check.sh`, `disk-usage-check.sh`, `cleanup-prometheus.sh` | System monitoring and alerts |
| **Maintenance** | `docker-cleanup.sh` | Periodic cleanup and maintenance |
| **Deployment** | `build-multiarch.sh` | Docker image building |

---

## Email Notification System

### lib/email-functions.sh

**Purpose**: Core email notification library providing SMTP integration, rate limiting, validation, and observability.

**Features**:
- Send emails via msmtp with retry logic (3 attempts, exponential backoff)
- Rate limiting: 10 emails/hour, 100 emails/day
- High-priority emails bypass hourly limit
- Input validation and sanitization
- Correlation ID generation for request tracing
- JSON structured logging
- Prometheus metrics integration
- Fallback alerting via syslog and ALERT file

**Functions**:

```bash
# Generate unique correlation ID
correlation_id=$(generate_correlation_id)

# Validate email address
if validate_email "test@example.com"; then
    echo "Valid"
fi

# Sanitize email content (remove shell metacharacters)
safe_content=$(sanitize_email_content "Hello; rm -rf /")

# Check rate limit (returns 0 if OK, 1 if limited)
if check_rate_limit "normal"; then
    echo "OK to send"
fi

# Send email
send_email "Subject" "Body" "correlation-id" "priority"
# Returns: 0 (success), 1 (failure), 2 (rate limited)

# Fallback alert when email fails
alert_fallback "error" "Critical alert message"

# Update Prometheus metrics
update_prometheus_metrics "metric_name" "value" "labels"
```

**Environment Variables**:

```bash
EMAIL_FROM=workshop2tsuchiya.iris@gmail.com    # Sender address
EMAIL_TO=workshop2tsuchiya.iris@gmail.com      # Recipient address
EMAIL_ENABLED=true                             # Enable/disable emails
SMTP_TIMEOUT=30                                # SMTP timeout (seconds)
EMAIL_RATE_LIMIT_HOURLY=10                     # Hourly rate limit
EMAIL_RATE_LIMIT_DAILY=100                     # Daily rate limit
EMAIL_LOG_DIR=/var/log/catchup                 # Log directory
```

**Usage Example**:

```bash
#!/bin/bash
source scripts/lib/email-functions.sh

# Generate correlation ID
CORR_ID=$(generate_correlation_id)

# Send email
if send_email "Test Subject" "Test Body" "$CORR_ID" "normal"; then
    echo "Email sent successfully"
else
    echo "Email failed"
fi
```

**Log Files**:
- `/var/log/catchup/email.log` - JSON-formatted email activity log
- `/var/log/catchup/email-rate-limit.log` - Rate limit tracking
- `/var/log/catchup/ALERT` - Fallback alerts when email fails

**Prometheus Metrics**:
- `catchup_email_sent_total{status="success|failure",priority="high|normal|low"}`
- `catchup_email_rate_limited_total{priority="high|normal|low"}`
- `catchup_email_latency_ms{priority="high|normal|low"}`

---

### setup-email.sh

**Purpose**: Interactive setup and verification script for the email notification system.

**Features**:
- Verifies msmtp installation
- Checks configuration file (`~/.msmtprc`) and permissions
- Tests SMTP connectivity to Gmail
- Sets up log directories with correct permissions
- Validates email addresses
- Sends test email to verify end-to-end delivery

**Usage**:

```bash
./scripts/setup-email.sh
```

**Exit Codes**:
- `0`: Success (all checks passed, test email sent)
- `1`: msmtp not found
- `2`: Configuration error (missing file, wrong permissions)
- `3`: SMTP connectivity failed
- `4`: Test email delivery failed

**Example Output**:

```
==============================================
CatchUp Feed - Email System Setup
==============================================

Step 1: Verifying msmtp installation...
✓ msmtp found: /usr/bin/msmtp

Step 2: Checking msmtp configuration...
✓ ~/.msmtprc exists
✓ Permissions: 600 (correct)

Step 3: Testing SMTP connectivity...
✓ Connection to smtp.gmail.com:587 successful

Step 4: Setting up log directories...
✓ /var/log/catchup created

Step 5: Validating email configuration...
✓ EMAIL_FROM: workshop2tsuchiya.iris@gmail.com
✓ EMAIL_TO: workshop2tsuchiya.iris@gmail.com

Step 6: Sending test email...
✓ Test email sent successfully

==============================================
Setup Complete! Email system is ready.
==============================================
```

**Dependencies**:
- msmtp (installed via `apt install msmtp msmtp-mta`)
- Gmail account with App Password
- Internet connectivity

---

## Database Management

### backup.sh

**Purpose**: Automated PostgreSQL database backup with comprehensive email notifications.

**Features**:
- Full database backup using `pg_dumpall`
- gzip compression for space efficiency
- Configurable retention policy (default: 7 days)
- Email notifications on success AND failure
- Disk usage monitoring and warnings
- Pre-flight checks (container running, connectivity)
- Correlation ID for request tracing
- Detailed error messages with troubleshooting steps

**Usage**:

```bash
# Default backup (7 days retention, compressed)
./scripts/backup.sh

# Custom retention period
./scripts/backup.sh --retention 14

# Custom output directory
./scripts/backup.sh --output ~/my-backups

# Disable compression
./scripts/backup.sh --no-compress

# Disable email notifications
./scripts/backup.sh --no-email
```

**Options**:

| Option | Description | Default |
|--------|-------------|---------|
| `--retention DAYS` | Number of days to keep backups | 7 |
| `--output DIR` | Backup output directory | `~/backups` |
| `--compress` | Enable gzip compression | true |
| `--no-compress` | Disable compression | - |
| `--no-email` | Disable email notifications | - |

**Environment Variables**:

```bash
POSTGRES_USER=catchup         # PostgreSQL username
POSTGRES_DB=catchup           # Database name
EMAIL_ENABLED=true            # Enable/disable email
EMAIL_FROM=...                # Sender email
EMAIL_TO=...                  # Recipient email
```

**Backup File Naming**:

```
db_YYYYMMDD_HHMMSS.sql.gz
# Example: db_20240115_020005.sql.gz
```

**Log File**: `~/backups/backup.log` (JSON format)

**Email Notifications**:

**Success Email**:
```
Subject: Database Backup Completed - 2024-01-15 02:00

Backup Details:
- File: db_20240115_020005.sql.gz
- Size: 2.3 MB (2,405,123 bytes)
- Duration: 12 seconds
- Old backups deleted: 1
- Disk usage: 45%

Current Backups: 7 file(s)
```

**Failure Email**:
```
Subject: Database Backup Failed - 2024-01-15 02:00

Error: pg_dumpall command failed with exit code 1

Troubleshooting:
1. Check PostgreSQL container status
2. Verify database credentials
3. Check container logs
4. Ensure database is accepting connections
```

**Restore Instructions**:

```bash
# Restore compressed backup
gunzip -c ~/backups/db_20240115_020005.sql.gz | \
  docker compose exec -T postgres psql -U catchup

# Restore uncompressed backup
docker compose exec -T postgres psql -U catchup < ~/backups/db_20240115_020005.sql
```

**Cron Example**:

```cron
# Daily at 2 AM
0 2 * * * /home/ubuntu/catchup-feed/scripts/backup.sh >> ~/backups/backup-cron.log 2>&1
```

---

### backup-db.sh

**Purpose**: Simple database backup script without email notifications.

**Usage**:

```bash
./scripts/backup-db.sh
```

**Note**: This script is a simplified version of `backup.sh` without email integration. Use `backup.sh` for production deployments.

---

### restore-db.sh

**Purpose**: Database restore utility with validation and confirmation prompts.

**Usage**:

```bash
# Interactive restore (prompts for confirmation)
./scripts/restore-db.sh /path/to/backup.sql.gz

# Force restore (no confirmation)
./scripts/restore-db.sh /path/to/backup.sql.gz --force
```

**Features**:
- Validates backup file exists and is readable
- Checks PostgreSQL container is running
- Prompts for confirmation before restoring
- Supports compressed (.gz) and uncompressed (.sql) backups
- Stops application containers before restore
- Restarts application after successful restore

**Safety**: Always test restore in a non-production environment first!

---

## Monitoring Scripts

### health-check.sh

**Purpose**: Comprehensive service health monitoring with email alerts on failures.

**Monitored Services**:
1. Docker daemon status
2. Container health (app, worker, postgres, prometheus, grafana)
3. PostgreSQL connectivity (`pg_isready`)
4. API endpoint availability (HTTP health check)
5. Worker process status

**Alert Behavior**:
- **Silent on success** (no email sent)
- **Alert on failure** (critical priority email with detailed status)

**Usage**:

```bash
./scripts/health-check.sh
```

**Exit Codes**:
- `0`: All services healthy (no alert)
- `1`: One or more services unhealthy (alert sent)

**Environment Variables**:

```bash
API_ENDPOINT=http://localhost:8080/health  # API health endpoint
API_TIMEOUT=5                              # API timeout (seconds)
EMAIL_ENABLED=true                         # Enable/disable email
```

**Email Alert Format**:

```
Subject: ❌ Health Check Failed on raspberrypi

Failed Services (2):
- app Container
- API Endpoint

All Services Status:
✓ Docker Daemon: Running
✗ app Container: Not running (status: exited)
✓ worker Container: Running
✓ postgres Container: Running (health: healthy)
✗ API Endpoint: Connection failed
✓ Worker Process: Running

Troubleshooting:
1. Check container logs: docker compose logs --tail=50 app
2. Restart failed services: docker compose restart app
3. Check service status: docker compose ps
```

**Cron Example**:

```cron
# Every 5 minutes
*/5 * * * * /home/ubuntu/catchup-feed/scripts/health-check.sh >> /var/log/catchup/health-check-cron.log 2>&1
```

**Dependencies**:
- Docker and Docker Compose
- curl (for API endpoint checks)

---

### disk-usage-check.sh

**Purpose**: Monitor filesystem disk usage and send tiered alerts based on thresholds.

**Thresholds**:
- **Warning**: 75% usage → Normal priority email
- **Critical**: 85% usage → High priority email
- **Silent**: < 75% → No email

**Monitored Filesystems**:
- `/` (root)
- `/var`
- `/home`
- `/var/lib/docker`

**Features**:
- Checks all filesystems in one run
- Sends ONE consolidated email for all issues
- Includes top 10 space consumers for problematic filesystems
- Provides actionable cleanup recommendations
- JSON structured logging with correlation ID

**Usage**:

```bash
./scripts/disk-usage-check.sh
```

**Exit Codes**:
- `0`: Success (check completed, with or without alerts)
- `1`: Missing dependency (email-functions.sh)
- `2`: Email sending failed

**Email Alert Format**:

```
Subject: ⚠️ Disk Usage Alert on raspberrypi

Warning Filesystems (1):
- /var: 78% used (15.6 GB / 20.0 GB)

Top Space Consumers on /var:
 1. 8.5G    /var/lib/docker
 2. 3.2G    /var/log
 3. 1.8G    /var/cache
 4. 0.9G    /var/lib/postgresql

Recommended Actions:
1. Clean Docker resources:
   docker compose down
   docker system prune -af --volumes

2. Clean old logs:
   sudo find /var/log -name "*.log" -mtime +30 -delete
   sudo journalctl --vacuum-time=7d

3. Clean apt cache:
   sudo apt clean
   sudo apt autoremove
```

**Cron Example**:

```cron
# Every 6 hours
0 */6 * * * /home/ubuntu/catchup-feed/scripts/disk-usage-check.sh >> /var/log/catchup/disk-usage-cron.log 2>&1
```

**Configuration**:

Adjust thresholds by editing the script:

```bash
WARNING_THRESHOLD=75   # Warning at 75%
CRITICAL_THRESHOLD=85  # Critical at 85%
```

---

### cleanup-prometheus.sh

**Purpose**: Monitor Prometheus data size and send warnings when thresholds are exceeded.

**Thresholds**:
- **Warning**: 2GB → Normal priority email
- **Critical**: 5GB → High priority email
- **Silent**: < 2GB → No email

**Features**:
- Calculates Prometheus data directory size
- Identifies oldest TSDB block timestamp
- Provides current retention policy from compose.yml
- Shows available disk space
- Recommends retention policy adjustments

**Usage**:

```bash
./scripts/cleanup-prometheus.sh
```

**Environment Variables**:

```bash
COMPOSE_FILE=./compose.yml                    # Path to compose.yml
PROJECT_ROOT=/home/ubuntu/catchup-feed        # Project root
EMAIL_FROM=...                                # Sender email
EMAIL_TO=...                                  # Recipient email
EMAIL_ENABLED=true                            # Enable/disable email
```

**Email Alert Format**:

```
Subject: ⚠️ Prometheus Data Size Warning - 3.45 GB

Current Status:
- Prometheus data size: 3.45 GB
- Warning threshold: 2.0 GB
- Critical threshold: 5.0 GB
- Oldest data: 2024-01-01 00:00:00 (14 days old)
- Available disk space: 25.3 GB (67% free)

Current Configuration:
- Retention policy: 30d
- Volume name: catchup-feed_prometheus-data

Recommended Actions:
1. Reduce retention period in compose.yml:
   prometheus:
     command:
       - '--storage.tsdb.retention.time=15d'

2. Restart Prometheus to apply changes:
   docker compose restart prometheus

3. Manually delete old data (if urgent):
   docker compose exec prometheus \
     promtool tsdb delete-blocks --retention.time=15d /prometheus
```

**Cron Example**:

```cron
# Every 6 hours
0 */6 * * * /home/ubuntu/catchup-feed/scripts/cleanup-prometheus.sh >> /var/log/catchup/prometheus-cleanup-cron.log 2>&1
```

**Prometheus Metrics**:
- `catchup_prometheus_data_size_bytes` - Data size in bytes
- `catchup_prometheus_data_size_gb` - Data size in GB

---

## Maintenance Scripts

### docker-cleanup.sh

**Purpose**: Automated Docker resource cleanup with detailed email summary report.

**Cleanup Operations**:
1. Remove unused images (7+ days old)
2. Remove unused volumes (preserves labeled volumes)
3. Clean build cache (7+ days old)
4. System prune (stopped containers, networks)

**Features**:
- Collects before/after statistics
- Calculates total space reclaimed
- Sends email report (always, even if nothing cleaned)
- Safe cleanup (preserves running containers and active volumes)
- Uses 7-day filter to avoid removing recently used resources

**Usage**:

```bash
./scripts/docker-cleanup.sh
```

**Email Report Format**:

```
Subject: Docker Cleanup Report - raspberrypi - 2024-01-15

Summary:
- Space reclaimed: 4.2 GB
- Images removed: 12
- Volumes removed: 3
- Build cache freed: 1.8 GB

Before Cleanup:
- Images: 23 (total size: 4.8GB)
- Containers: 5 (total size: 125MB)
- Local Volumes: 8 (total size: 2.1GB)
- Build Cache: 1.8GB

After Cleanup:
- Images: 11 (total size: 2.3GB)
- Containers: 5 (total size: 125MB)
- Local Volumes: 5 (total size: 1.5GB)
- Build Cache: 0B

Current Disk Usage:
TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          11        5         2.3GB     1.2GB (52%)
Containers      5         5         125MB     0B (0%)
Local Volumes   5         5         1.5GB     0B (0%)
Build Cache     0         0         0B        0B
```

**Cron Example**:

```cron
# Weekly on Sunday at 3 AM
0 3 * * 0 /home/ubuntu/catchup-feed/scripts/docker-cleanup.sh >> /var/log/catchup/docker-cleanup-cron.log 2>&1
```

**Safety Notes**:
- Does NOT remove running containers
- Does NOT remove volumes in use
- Uses `--filter "until=168h"` (7 days) to preserve recent resources
- Always sends email report for audit trail

---

## Deployment Scripts

### build-multiarch.sh

**Purpose**: Build multi-architecture Docker images for ARM64 (Raspberry Pi) and AMD64 (x86_64).

**Usage**:

```bash
# Build for current platform
./scripts/build-multiarch.sh

# Build for specific platform
./scripts/build-multiarch.sh --platform linux/arm64

# Build for multiple platforms
./scripts/build-multiarch.sh --platform linux/arm64,linux/amd64
```

**Features**:
- Supports Docker buildx for multi-platform builds
- Tags images with architecture suffix
- Pushes to Docker registry (if configured)
- Validates Dockerfile before build

**Supported Platforms**:
- `linux/arm64` - Raspberry Pi 4/5, AWS Graviton
- `linux/amd64` - x86_64 servers, development machines

**Note**: Requires Docker buildx and QEMU for cross-platform builds.

---

## Environment Variables

All scripts use the following environment variables (defined in `.env`):

### Database Configuration

```bash
POSTGRES_USER=catchup          # PostgreSQL username
POSTGRES_DB=catchup            # Database name
POSTGRES_PASSWORD=...          # Database password
```

### Email Configuration

```bash
EMAIL_ENABLED=true                             # Master switch
EMAIL_FROM=workshop2tsuchiya.iris@gmail.com    # Sender address
EMAIL_TO=workshop2tsuchiya.iris@gmail.com      # Recipient address
SMTP_TIMEOUT=30                                # SMTP timeout (seconds)
EMAIL_RATE_LIMIT_HOURLY=10                     # Hourly rate limit
EMAIL_RATE_LIMIT_DAILY=100                     # Daily rate limit
EMAIL_LOG_DIR=/var/log/catchup                 # Log directory
```

### Monitoring Configuration

```bash
API_ENDPOINT=http://localhost:8080/health      # Health check endpoint
API_TIMEOUT=5                                  # API timeout (seconds)
```

### Prometheus Configuration

```bash
PROMETHEUS_METRICS_DIR=/var/lib/node_exporter/textfile_collector
```

---

## Cron Schedule Recommendations

### Production Environment (Raspberry Pi 5)

```cron
# Email system is already configured, no need to re-run setup
# Setup: (one-time) ./scripts/setup-email.sh

# Database backup - Daily at 2 AM
0 2 * * * /home/ubuntu/catchup-feed/scripts/backup.sh >> /home/ubuntu/backups/backup-cron.log 2>&1

# Health check - Every 5 minutes
*/5 * * * * /home/ubuntu/catchup-feed/scripts/health-check.sh >> /var/log/catchup/health-check-cron.log 2>&1

# Disk usage check - Every 6 hours
0 */6 * * * /home/ubuntu/catchup-feed/scripts/disk-usage-check.sh >> /var/log/catchup/disk-usage-cron.log 2>&1

# Prometheus cleanup - Every 6 hours
0 */6 * * * /home/ubuntu/catchup-feed/scripts/cleanup-prometheus.sh >> /var/log/catchup/prometheus-cleanup-cron.log 2>&1

# Docker cleanup - Weekly on Sunday at 3 AM
0 3 * * 0 /home/ubuntu/catchup-feed/scripts/docker-cleanup.sh >> /var/log/catchup/docker-cleanup-cron.log 2>&1
```

### Development Environment

```cron
# Less frequent monitoring for development

# Database backup - Daily at 3 AM
0 3 * * * /path/to/catchup-feed/scripts/backup.sh --retention 3

# Health check - Every 30 minutes
*/30 * * * * /path/to/catchup-feed/scripts/health-check.sh

# Disk usage check - Daily
0 12 * * * /path/to/catchup-feed/scripts/disk-usage-check.sh

# Docker cleanup - Weekly
0 4 * * 0 /path/to/catchup-feed/scripts/docker-cleanup.sh
```

### Installing Cron Jobs

```bash
# Edit crontab
crontab -e

# Paste the schedule above

# Verify cron jobs
crontab -l

# Check cron service is running
sudo systemctl status cron
```

---

## Correlation ID Usage

### What is a Correlation ID?

A Correlation ID is a unique identifier that traces a request across multiple systems and log files.

**Format**: `{timestamp}-{hostname}-{pid}-{random_hex}`

**Example**: `1705252805-raspberrypi-12345-a3f8c2d1`

### Why Use Correlation IDs?

1. **Trace requests**: Follow a single operation across multiple log files
2. **Debug issues**: Link email logs, script logs, and Prometheus metrics
3. **Audit trail**: Track who did what and when
4. **Support cases**: Provide correlation ID for faster troubleshooting

### How Scripts Use Correlation IDs

All monitoring scripts generate a correlation ID at the start:

```bash
#!/bin/bash
source scripts/lib/email-functions.sh

# Generate correlation ID
CORRELATION_ID=$(generate_correlation_id)

# Use in logging
log_json "info" "Starting backup" "$CORRELATION_ID"

# Use in email
send_email "Backup Complete" "Details..." "$CORRELATION_ID" "normal"
```

### Finding Logs by Correlation ID

```bash
# Search email log
grep "1705252805-raspberrypi-12345-a3f8c2d1" /var/log/catchup/email.log

# Search backup log
grep "1705252805-raspberrypi-12345-a3f8c2d1" ~/backups/backup.log

# Search across all logs
grep -r "1705252805-raspberrypi-12345-a3f8c2d1" /var/log/catchup/
```

### Correlation ID in Emails

Every email includes the correlation ID in the footer:

```
Timestamp: 2024-01-15T02:00:17+09:00
Hostname: raspberrypi
Correlation ID: 1705252805-raspberrypi-12345-a3f8c2d1
```

This allows you to quickly find related logs when investigating an issue.

---

## Integration with Email Notification System

All scripts integrate with the email notification system via `lib/email-functions.sh`:

```bash
#!/bin/bash

# Source email functions library
source "$(dirname "$0")/lib/email-functions.sh"

# Generate correlation ID
CORR_ID=$(generate_correlation_id)

# Perform operation
perform_backup() {
    # ... backup logic ...
}

# Send notification
if perform_backup; then
    send_email \
        "Backup Successful" \
        "Backup completed at $(date)" \
        "$CORR_ID" \
        "normal"
else
    send_email \
        "Backup Failed" \
        "Backup failed. Check logs." \
        "$CORR_ID" \
        "high"
fi
```

---

## Troubleshooting

### Script Fails with "Permission denied"

**Solution**: Ensure scripts are executable

```bash
chmod +x scripts/*.sh
```

### Email Not Sending

**Diagnosis**:

```bash
# Check email.log
tail -20 /var/log/catchup/email.log | python3 -m json.tool

# Run email setup
./scripts/setup-email.sh

# Check msmtp log
tail -20 ~/.msmtp.log
```

**See**: [Email Notification Troubleshooting Runbook](../docs/runbooks/email-notification-troubleshooting.md)

### Cron Jobs Not Running

**Diagnosis**:

```bash
# Check cron service
sudo systemctl status cron

# View cron jobs
crontab -l

# Check syslog for cron errors
grep CRON /var/log/syslog
```

**Solution**: Ensure full paths are used in crontab (cron doesn't inherit environment)

---

## Additional Resources

- **Email Notification User Guide**: `docs/guides/email-notification-user-guide.md`
- **Troubleshooting Runbook**: `docs/runbooks/email-notification-troubleshooting.md`
- **Deployment Guide**: `task.md`

---

**Last Updated**: 2025-01-18
**Maintainer**: CatchUp Feed Team
