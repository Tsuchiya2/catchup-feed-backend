#!/usr/bin/env bash
# ============================================================
# CatchUp Feed - Email System Setup & Verification Script
# ============================================================
# Interactive setup script that verifies msmtp configuration
# and tests email delivery.
#
# Features:
#   - msmtp installation and configuration verification
#   - SMTP connectivity testing
#   - Log directory setup
#   - Email validation
#   - Test email sending with detailed reporting
#
# Exit Codes:
#   0: Success
#   1: msmtp not found
#   2: Configuration error (missing/wrong permissions)
#   3: SMTP connectivity failed
#   4: Test email failed
#
# Usage:
#   ./scripts/setup-email.sh
# ============================================================

set -euo pipefail

# ============================================================
# Configuration
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
EMAIL_FUNCTIONS="${SCRIPT_DIR}/lib/email-functions.sh"

# Environment variables (with defaults)
EMAIL_FROM="${EMAIL_FROM:-workshop2tsuchiya.iris@gmail.com}"
EMAIL_TO="${EMAIL_TO:-workshop2tsuchiya.iris@gmail.com}"
EMAIL_LOG_DIR="${EMAIL_LOG_DIR:-/var/log/catchup}"

# ============================================================
# Color Codes
# ============================================================

# Use tput for portable color support
if command -v tput >/dev/null 2>&1 && [ -t 1 ]; then
    COLOR_GREEN=$(tput setaf 2)
    COLOR_RED=$(tput setaf 1)
    COLOR_YELLOW=$(tput setaf 3)
    COLOR_BLUE=$(tput setaf 4)
    COLOR_BOLD=$(tput bold)
    COLOR_RESET=$(tput sgr0)
else
    # Fallback to ANSI codes if tput not available
    COLOR_GREEN='\033[0;32m'
    COLOR_RED='\033[0;31m'
    COLOR_YELLOW='\033[0;33m'
    COLOR_BLUE='\033[0;34m'
    COLOR_BOLD='\033[1m'
    COLOR_RESET='\033[0m'
fi

# ============================================================
# Utility Functions
# ============================================================

# Print section header
print_header() {
    echo ""
    echo "${COLOR_BLUE}${COLOR_BOLD}=============================================="
    echo "$1"
    echo "==============================================${COLOR_RESET}"
    echo ""
}

# Print step with number
print_step() {
    echo ""
    echo "${COLOR_BOLD}Step $1: $2${COLOR_RESET}"
}

# Print success message
print_success() {
    echo "${COLOR_GREEN}✓${COLOR_RESET} $1"
}

# Print error message
print_error() {
    echo "${COLOR_RED}✗${COLOR_RESET} $1"
}

# Print warning message
print_warning() {
    echo "${COLOR_YELLOW}⚠${COLOR_RESET} $1"
}

# Print info message
print_info() {
    echo "  ${COLOR_BLUE}→${COLOR_RESET} $1"
}

# Exit with error
exit_with_error() {
    local exit_code=$1
    local message=$2
    echo ""
    print_error "$message"
    echo ""
    exit "$exit_code"
}

# ============================================================
# Verification Functions
# ============================================================

# Step 1: Verify msmtp installation
verify_msmtp_installation() {
    print_step "1" "Verifying msmtp installation..."

    if ! command -v msmtp >/dev/null 2>&1; then
        print_error "msmtp not found"
        echo ""
        print_info "Install msmtp:"
        print_info "  macOS: brew install msmtp"
        print_info "  Debian/Ubuntu: sudo apt-get install msmtp"
        print_info "  Raspberry Pi: sudo apt-get install msmtp msmtp-mta"
        exit_with_error 1 "msmtp is required but not installed"
    fi

    local msmtp_path
    msmtp_path=$(command -v msmtp)
    print_success "msmtp found: ${msmtp_path}"
}

# Step 2: Check msmtp configuration
verify_msmtp_configuration() {
    print_step "2" "Checking msmtp configuration..."

    local msmtprc="$HOME/.msmtprc"

    # Check if configuration file exists
    if [ ! -f "$msmtprc" ]; then
        print_error "~/.msmtprc not found"
        echo ""
        print_info "Create ~/.msmtprc with the following content:"
        echo ""
        cat <<'EOF'
# Gmail SMTP Configuration
account gmail
host smtp.gmail.com
port 587
from workshop2tsuchiya.iris@gmail.com
auth on
user workshop2tsuchiya.iris@gmail.com
password YOUR_APP_PASSWORD_HERE
tls on
tls_starttls on
tls_trust_file /etc/ssl/certs/ca-certificates.crt
logfile ~/.msmtp.log

# Set default account
account default : gmail
EOF
        echo ""
        print_info "Then set permissions: chmod 600 ~/.msmtprc"
        exit_with_error 2 "Configuration file missing"
    fi

    print_success "~/.msmtprc exists"

    # Check file permissions (should be 600)
    local perms
    perms=$(stat -f "%OLp" "$msmtprc" 2>/dev/null || stat -c "%a" "$msmtprc" 2>/dev/null)

    if [ "$perms" != "600" ]; then
        print_warning "Permissions: ${perms} (should be 600)"
        print_info "Fixing permissions..."
        chmod 600 "$msmtprc"
        print_success "Permissions set to 600"
    else
        print_success "Permissions: 600 (correct)"
    fi

    # Parse configuration
    print_success "Configuration parsed:"

    local account host port from_addr
    account=$(grep -E "^account " "$msmtprc" | head -n 1 | awk '{print $2}')
    host=$(grep -E "^host " "$msmtprc" | head -n 1 | awk '{print $2}')
    port=$(grep -E "^port " "$msmtprc" | head -n 1 | awk '{print $2}')
    from_addr=$(grep -E "^from " "$msmtprc" | head -n 1 | awk '{print $2}')

    print_info "Account: ${account:-<not set>}"
    print_info "From: ${from_addr:-<not set>}"
    print_info "Host: ${host:-<not set>}"
    print_info "Port: ${port:-<not set>}"

    # Validate that required fields are present
    if [ -z "$host" ] || [ -z "$port" ] || [ -z "$from_addr" ]; then
        exit_with_error 2 "Incomplete configuration in ~/.msmtprc"
    fi
}

# Step 3: Test SMTP connectivity
test_smtp_connectivity() {
    print_step "3" "Testing SMTP connectivity..."

    local smtp_host="smtp.gmail.com"
    local smtp_port="587"

    # Test connection using bash /dev/tcp with timeout
    if timeout 5 bash -c "exec 3<>/dev/tcp/${smtp_host}/${smtp_port}" 2>/dev/null; then
        # Close the connection
        exec 3>&-
        print_success "Connection to ${smtp_host}:${smtp_port} successful"
    else
        print_error "Connection to ${smtp_host}:${smtp_port} failed"
        echo ""
        print_info "Troubleshooting:"
        print_info "  1. Check your internet connection"
        print_info "  2. Verify firewall settings"
        print_info "  3. Confirm Gmail SMTP is accessible"
        exit_with_error 3 "SMTP connectivity test failed"
    fi
}

# Step 4: Setup log directories
setup_log_directories() {
    print_step "4" "Setting up log directories..."

    # Create main log directory
    if [ ! -d "$EMAIL_LOG_DIR" ]; then
        if mkdir -p "$EMAIL_LOG_DIR" 2>/dev/null; then
            print_success "${EMAIL_LOG_DIR} created"
        else
            print_warning "Cannot create ${EMAIL_LOG_DIR} (permission denied)"
            print_info "Falling back to ${PROJECT_ROOT}/logs"
            EMAIL_LOG_DIR="${PROJECT_ROOT}/logs"
            export EMAIL_LOG_DIR
            mkdir -p "$EMAIL_LOG_DIR"
            print_success "${EMAIL_LOG_DIR} created"
        fi
    else
        print_success "${EMAIL_LOG_DIR} exists"
    fi

    # Set permissions if possible
    if chmod 775 "$EMAIL_LOG_DIR" 2>/dev/null; then
        print_success "Permissions set to 775"
    else
        print_warning "Cannot set permissions (continuing anyway)"
    fi

    # Create subdirectories if needed
    for subdir in "metrics" "backups"; do
        local dir="${EMAIL_LOG_DIR}/${subdir}"
        if [ ! -d "$dir" ]; then
            mkdir -p "$dir" 2>/dev/null || true
        fi
    done
}

# Step 5: Validate email configuration
validate_email_configuration() {
    print_step "5" "Validating email configuration..."

    # Source email functions to use validate_email function
    if [ ! -f "$EMAIL_FUNCTIONS" ]; then
        exit_with_error 2 "Email functions library not found: ${EMAIL_FUNCTIONS}"
    fi

    # Source the library
    source "$EMAIL_FUNCTIONS"

    # Validate EMAIL_FROM
    if validate_email "$EMAIL_FROM"; then
        print_success "EMAIL_FROM: ${EMAIL_FROM}"
    else
        print_error "Invalid EMAIL_FROM: ${EMAIL_FROM}"
        exit_with_error 2 "Invalid sender email address"
    fi

    # Validate EMAIL_TO
    if validate_email "$EMAIL_TO"; then
        print_success "EMAIL_TO: ${EMAIL_TO}"
    else
        print_error "Invalid EMAIL_TO: ${EMAIL_TO}"
        exit_with_error 2 "Invalid recipient email address"
    fi
}

# Step 6: Send test email
send_test_email() {
    print_step "6" "Sending test email..."

    # Source email functions if not already sourced
    if ! type -t send_email >/dev/null 2>&1; then
        source "$EMAIL_FUNCTIONS"
    fi

    # Generate test email content
    local hostname
    hostname=$(hostname -s 2>/dev/null || hostname)
    local timestamp
    timestamp=$(date -Iseconds)
    local correlation_id
    correlation_id=$(generate_correlation_id)

    local subject="CatchUp Feed - Email Test"
    local body="This is a test email from setup-email.sh

Hostname: ${hostname}
Timestamp: ${timestamp}
Correlation ID: ${correlation_id}

If you received this email, the email system is working correctly!"

    # Send test email
    if send_email "$subject" "$body" "$correlation_id" "normal"; then
        print_success "Test email sent successfully"
        print_info "Correlation ID: ${correlation_id}"
        print_info "Check your inbox: ${EMAIL_TO}"

        # Check email log for confirmation
        local email_log="${EMAIL_LOG_DIR}/email.log"
        if [ -f "$email_log" ]; then
            echo ""
            print_info "Last email log entry:"
            tail -n 1 "$email_log" | python3 -m json.tool 2>/dev/null || tail -n 1 "$email_log"
        fi
    else
        print_error "Failed to send test email"
        echo ""
        print_info "Check logs for details:"
        print_info "  Email log: ${EMAIL_LOG_DIR}/email.log"
        print_info "  msmtp log: ~/.msmtp.log"

        # Show last few log entries if available
        local email_log="${EMAIL_LOG_DIR}/email.log"
        if [ -f "$email_log" ]; then
            echo ""
            print_info "Recent email log entries:"
            tail -n 3 "$email_log"
        fi

        exit_with_error 4 "Test email delivery failed"
    fi
}

# ============================================================
# Main Execution
# ============================================================

main() {
    print_header "CatchUp Feed - Email System Setup"

    # Run all verification steps
    verify_msmtp_installation
    verify_msmtp_configuration
    test_smtp_connectivity
    setup_log_directories
    validate_email_configuration
    send_test_email

    # Success summary
    echo ""
    print_header "Setup Complete! Email system is ready."
    echo ""
    print_info "Configuration:"
    print_info "  From: ${EMAIL_FROM}"
    print_info "  To: ${EMAIL_TO}"
    print_info "  Log directory: ${EMAIL_LOG_DIR}"
    echo ""
    print_info "Next steps:"
    print_info "  1. Check your inbox for the test email"
    print_info "  2. Review logs: tail -f ${EMAIL_LOG_DIR}/email.log"
    print_info "  3. Use send_email() function in your scripts"
    echo ""
}

# Run main function
main
