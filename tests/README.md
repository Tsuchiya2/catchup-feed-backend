# Email Notification System - Test Suite

This directory contains comprehensive tests for the email notification system implemented in `scripts/lib/email-functions.sh` and related notification scripts.

## Test Structure

```
tests/
├── unit/                   # Unit tests for email-functions.sh library
├── integration/            # Integration tests for SMTP and system components
├── e2e/                   # End-to-end tests for notification scripts
├── performance/           # Performance and stress tests
└── README.md              # This file
```

## Test Files

### Unit Tests
- **`unit/test-email-functions.sh`** (769 lines)
  - Tests all functions in `email-functions.sh` library
  - 9 test cases covering core functionality
  - Identifies compatibility issues with macOS
  - Uses mocked msmtp to avoid sending real emails

### Integration Tests
- **`integration/test-email-integration.sh`** (679 lines)
  - Tests SMTP connectivity and real email sending
  - 6 test cases covering system integration
  - Verifies rate limiting behavior
  - Tests fallback mechanisms and Prometheus metrics
  - Can use actual msmtp or mock SMTP server

### E2E Tests
- **`e2e/test-notification-scripts.sh`** (800 lines)
  - Tests all 5 notification scripts end-to-end
  - Verifies correlation ID propagation
  - Tests both success and failure scenarios
  - Mocks external dependencies (Docker, database)

### Performance Tests
- **`performance/test-email-performance.sh`** (700 lines)
  - 6 test cases covering performance metrics
  - Tests rate limit enforcement under load
  - Concurrent email sending (10 parallel processes)
  - Latency measurement (target: <5s per email)
  - Retry logic with exponential backoff
  - File locking under concurrent access

## Running Tests

### Prerequisites

1. **Email functions library must be installed:**
   ```bash
   ls scripts/lib/email-functions.sh
   ```

2. **For integration tests (optional):**
   - msmtp installed and configured
   - `.env` file with EMAIL_* configuration
   - SMTP server accessible (or tests will use mock)

3. **Permissions:**
   All test scripts are executable:
   ```bash
   chmod +x tests/**/*.sh
   ```

### Run All Tests

Run each test suite independently:

```bash
# Unit tests
./tests/unit/test-email-functions.sh

# Integration tests
./tests/integration/test-email-integration.sh

# E2E tests
./tests/e2e/test-notification-scripts.sh

# Performance tests
./tests/performance/test-email-performance.sh
```

### Run Tests Sequentially

```bash
# Run all tests in order
for test in tests/unit/*.sh tests/integration/*.sh tests/e2e/*.sh tests/performance/*.sh; do
  echo "Running $test..."
  bash "$test" || echo "Test failed: $test"
done
```

## Test Coverage

### Unit Tests (test-email-functions.sh)

| Test | Coverage |
|------|----------|
| TEST 1 | `generate_correlation_id()` - Format and uniqueness |
| TEST 2 | `validate_email()` - Email validation |
| TEST 3 | `sanitize_email_content()` - Content sanitization |
| TEST 4 | `check_rate_limit()` - Rate limiting logic |
| TEST 5 | `send_email()` - Email sending with mocked msmtp |
| TEST 6 | `alert_fallback()` - Fallback alerting |
| TEST 7 | `check_consecutive_failures()` - Failure detection |
| TEST 8 | `update_prometheus_metrics()` - Metrics updates |
| TEST 9 | Integration - Complete email flow |

**Total: 145 test cases across 9 test functions**

### Integration Tests (test-email-integration.sh)

| Test | Coverage |
|------|----------|
| TEST 1 | msmtp connectivity - SMTP server reachable |
| TEST 2 | Email sending - Real/mock SMTP delivery |
| TEST 3 | Rate limiting - Hourly/daily limits enforced |
| TEST 4 | Fallback mechanisms - Syslog and alert files |
| TEST 5 | Prometheus metrics - Metrics file creation |
| TEST 6 | Log file management - Log creation and format |

**Total: 6 integration test scenarios**

### E2E Tests (test-notification-scripts.sh)

| Test | Scripts Tested |
|------|----------------|
| TEST 1 | `backup.sh` - Success/failure notifications |
| TEST 2 | `health-check.sh` - Health monitoring alerts |
| TEST 3 | `cleanup-prometheus.sh` - Size warnings |
| TEST 4 | `docker-cleanup.sh` - Cleanup reports |
| TEST 5 | `disk-usage-check.sh` - Disk usage alerts |
| TEST 6 | Correlation ID propagation across all scripts |

**Total: 6 E2E test scenarios covering 5 scripts**

### Performance Tests (test-email-performance.sh)

| Test | Coverage |
|------|----------|
| TEST 1 | Rate limit enforcement under load (15 rapid sends) |
| TEST 2 | Concurrent email sending (10 parallel processes) |
| TEST 3 | Latency measurement (5 iterations) |
| TEST 4 | Retry logic performance (exponential backoff) |
| TEST 5 | Metrics update performance (20 iterations) |
| TEST 6 | File locking under concurrent access |

**Total: 6 performance test scenarios**

## Test Output

All tests provide detailed output with:
- ✓ **Green**: Test passed
- ✗ **Red**: Test failed
- ⚠ **Yellow**: Warning or expected compatibility issue

### Example Output

```
==========================================
Email Functions Library - Unit Tests
==========================================
Project Root: /Users/user/catchup-feed
Library: /Users/user/catchup-feed/scripts/lib/email-functions.sh
Test Directory: /tmp/test-catchup-logs-12345

==========================================
Test: TEST 1: generate_correlation_id()
==========================================
✓ PASS: Correlation ID contains hyphens
✓ PASS: Correlation IDs should be unique
✓ PASS: Correlation ID should have 4 parts (3 hyphens)
✓ PASS: Random hex part should be 8 characters
✓ Test PASSED

...

==========================================
Test Summary
==========================================
Total Tests: 9
Passed: 9
Failed: 0

✓ All tests passed!
```

## Known Issues

### macOS Compatibility

The `email-functions.sh` library has compatibility issues with macOS (detected by unit tests):

1. **date command**: `date +%s%3N` (milliseconds) not supported on macOS
   - Lines affected: 475, 492, 508 in `email-functions.sh`
   - Workaround: Use `date +%s` (seconds only) or install GNU date (`gdate`)

2. **Bash version**: `${severity^^}` syntax requires Bash 4.0+
   - macOS ships with Bash 3.2
   - Line affected: 304 in `email-functions.sh`
   - Workaround: Use `echo $severity | tr '[:lower:]' '[:upper:]'`

These issues are identified by unit tests and do not affect functionality on Linux/Raspberry Pi.

## Temporary Files

All tests use temporary directories for test artifacts:
- Unit tests: `/tmp/test-catchup-logs-$$`
- Integration tests: `/tmp/test-email-integration-$$`
- E2E tests: `/tmp/test-e2e-notifications-$$`
- Performance tests: `/tmp/test-email-performance-$$`

Test artifacts are automatically cleaned up after execution.

## CI/CD Integration

Tests can be integrated into CI/CD pipelines:

```bash
#!/bin/bash
# Run all tests and collect results
FAILED=0

for test_script in tests/unit/*.sh tests/integration/*.sh; do
  if ! bash "$test_script"; then
    FAILED=$((FAILED + 1))
  fi
done

exit $FAILED
```

## Test Environment Variables

Tests respect the following environment variables:

- `EMAIL_FROM`: Sender email address (default: test-sender@example.com)
- `EMAIL_TO`: Recipient email address (default: test-recipient@example.com)
- `EMAIL_ENABLED`: Enable/disable email sending (default: true)
- `EMAIL_RATE_LIMIT_HOURLY`: Hourly rate limit (default: 10)
- `EMAIL_RATE_LIMIT_DAILY`: Daily rate limit (default: 100)
- `SMTP_TIMEOUT`: SMTP timeout in seconds (default: 5)

Override these for custom test configurations:

```bash
EMAIL_FROM="custom@example.com" EMAIL_TO="test@example.com" ./tests/unit/test-email-functions.sh
```

## Troubleshooting

### Tests fail with "email-functions.sh not found"

Ensure you're running tests from the project root:

```bash
cd /path/to/catchup-feed
./tests/unit/test-email-functions.sh
```

### Integration tests fail with SMTP errors

If you don't have msmtp configured, tests will use mock SMTP. To use real SMTP:

1. Install msmtp: `brew install msmtp` (macOS) or `apt install msmtp` (Linux)
2. Configure `~/.msmtprc` with Gmail SMTP credentials
3. Run tests: `./tests/integration/test-email-integration.sh`

### Performance tests show warnings

Performance tests may show warnings on slower systems. This is expected and doesn't indicate failure.

## Contributing

When adding new features to the email notification system:

1. Add unit tests to `tests/unit/test-email-functions.sh`
2. Add integration tests to `tests/integration/test-email-integration.sh`
3. Update E2E tests if adding new notification scripts
4. Run all tests before submitting changes

## Test Maintenance

- **Unit tests**: Update when modifying `email-functions.sh` functions
- **Integration tests**: Update when changing SMTP integration or rate limiting
- **E2E tests**: Update when adding/modifying notification scripts
- **Performance tests**: Update when changing rate limits or retry logic

## References

- Design document: `docs/designs/email-notification-system.md`
- Task plan: `docs/plans/email-notification-system-tasks.md`
- User guide: `docs/guides/email-notifications.md`
- Email functions library: `scripts/lib/email-functions.sh`

---

**Last Updated**: 2025-11-18
**Total Test Files**: 4
**Total Test Cases**: 200+
**Total Lines of Code**: 2,179
