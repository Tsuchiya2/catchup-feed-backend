# Prometheus Alert Rules

This directory contains Prometheus alert rule definitions for the catchup-feed project.

## Alert Files

### catchup-alerts.yml
Main application alerts covering:
- API server health and performance
- Business metrics (summarization, article fetching)
- Database performance
- Notification system
- Infrastructure metrics

### email-alerts.yml
Email system-specific alerts:
- Email delivery failure rate monitoring
- Consecutive failure detection
- Rate limit warnings
- System health checks
- Fallback mechanism tracking
- Queue backlog monitoring
- Delivery latency alerts

## Testing Alert Rules

### Syntax Validation

Validate alert rule syntax using promtool:

```bash
# From host
docker compose exec prometheus promtool check rules /etc/prometheus/alerts/email-alerts.yml

# Expected output:
# Checking /etc/prometheus/alerts/email-alerts.yml
#   SUCCESS: 7 rules found
```

### Reload Configuration

After modifying alert rules, reload Prometheus configuration:

```bash
# Using API (if --web.enable-lifecycle is enabled)
curl -X POST http://localhost:9090/-/reload

# Or restart Prometheus
docker compose restart prometheus
```

### View Active Alerts

Check currently firing alerts:

```bash
# Via API
curl http://localhost:9090/api/v1/alerts | jq

# Via Web UI
open http://localhost:9090/alerts
```

## Alert Configuration

### Email Alerts Detail

#### 1. EmailDeliveryFailureRate
- **Threshold**: >10% failure rate over 1 hour
- **Duration**: 10 minutes
- **Severity**: Warning
- **Description**: Monitors email delivery success rate

#### 2. EmailConsecutiveFailures
- **Threshold**: 3+ failures with 0 successes in 15 minutes
- **Duration**: 5 minutes
- **Severity**: Critical
- **Description**: Detects systemic email delivery issues

#### 3. EmailRateLimitNearExceeded
- **Threshold**: ≥8 emails per hour (80% of limit)
- **Duration**: 5 minutes
- **Severity**: Warning
- **Description**: Warns when approaching hourly rate limit

#### 4. EmailSystemDown
- **Threshold**: No emails sent in 24+ hours (during active hours)
- **Duration**: 1 hour
- **Severity**: Critical
- **Description**: Detects complete email system failure

#### 5. EmailFallbackActive
- **Threshold**: >0 fallback activations in 1 hour
- **Duration**: 5 minutes
- **Severity**: Warning
- **Description**: Tracks when fallback notification methods are used

#### 6. EmailQueueBacklog
- **Threshold**: >5 pending emails
- **Duration**: 30 minutes
- **Severity**: Warning
- **Description**: Monitors email queue buildup

#### 7. EmailDeliveryLatencyHigh
- **Threshold**: 95th percentile >30 seconds
- **Duration**: 10 minutes
- **Severity**: Warning
- **Description**: Detects slow email delivery

## Required Metrics

The email alerts require the following metrics to be exposed by the application:

### Email Delivery Metrics
```promql
# Email sent counter (required)
catchup_email_sent_total{status="success|failure"}

# Email send duration histogram (required)
catchup_email_send_duration_seconds_bucket

# Rate limit gauge (required)
catchup_email_rate_limit_hourly_current

# Fallback counter (optional)
catchup_email_fallback_total{reason="..."}

# Queue gauge (optional)
catchup_email_queue_pending{priority="high|normal"}
```

### Implementation Example

```go
// In your email service
var (
    emailSentTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "catchup_email_sent_total",
            Help: "Total number of emails sent",
        },
        []string{"status"}, // "success" or "failure"
    )

    emailSendDuration = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "catchup_email_send_duration_seconds",
            Help:    "Email sending duration in seconds",
            Buckets: prometheus.DefBuckets,
        },
    )

    emailRateLimitCurrent = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "catchup_email_rate_limit_hourly_current",
            Help: "Current hourly email count",
        },
    )
)
```

## Troubleshooting

### Alert Not Firing

1. Check if metric exists:
   ```bash
   curl 'http://localhost:9090/api/v1/query?query=catchup_email_sent_total'
   ```

2. Test alert expression:
   ```bash
   curl 'http://localhost:9090/api/v1/query?query=EXPR_HERE'
   ```

3. Check alert state:
   ```bash
   curl http://localhost:9090/api/v1/rules | jq '.data.groups[] | select(.name=="email_alerts")'
   ```

### Alert Firing Too Often

Adjust thresholds or duration in the alert rule:

```yaml
- alert: EmailDeliveryFailureRate
  expr: (rate(...) / rate(...)) > 0.20  # Increase from 0.10 to 0.20
  for: 20m  # Increase from 10m to 20m
```

### False Positives

Use `unless` or `and` to add conditions:

```yaml
expr: |
  (rate(...) / rate(...)) > 0.10
  unless
  rate(catchup_email_sent_total[1h]) < 5  # Ignore if <5 emails sent
```

## Alertmanager Integration

To send alert notifications, configure Alertmanager in `prometheus.yml`:

```yaml
alerting:
  alertmanagers:
    - static_configs:
        - targets:
          - 'alertmanager:9093'
```

Then create `alertmanager.yml` with routes and receivers:

```yaml
route:
  group_by: ['alertname', 'severity']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'email'

receivers:
  - name: 'email'
    email_configs:
      - to: 'admin@example.com'
        from: 'alerts@example.com'
        smarthost: 'smtp.gmail.com:587'
        auth_username: 'alerts@example.com'
        auth_password: 'APP_PASSWORD'
```

## Best Practices

### Alert Design Principles

1. **Actionable**: Every alert should require action
2. **Clear**: Annotations should explain what's wrong and how to fix it
3. **Threshold**: Set thresholds to avoid noise
4. **Duration**: Use `for:` to prevent flapping alerts
5. **Severity**: Use appropriate severity levels

### Severity Levels

- **Critical**: Requires immediate attention (pages on-call)
  - Service completely down
  - Data loss risk
  - Security breach

- **Warning**: Requires attention within hours
  - Performance degradation
  - Approaching limits
  - Non-critical failures

### Testing Alerts

Before deploying new alerts:

1. Validate syntax with promtool
2. Test expression returns expected values
3. Verify alert fires when condition is met
4. Check alert resolves when condition clears
5. Review alert frequency and noise level

## References

- [Prometheus Alerting Rules](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/)
- [Alertmanager Documentation](https://prometheus.io/docs/alerting/latest/alertmanager/)
- [Prometheus Query Functions](https://prometheus.io/docs/prometheus/latest/querying/functions/)
- [My Philosophy on Alerting](https://docs.google.com/document/d/199PqyG3UsyXlwieHaqbGiWVa8eMWi8zzAn0YfcApr8Q/edit) (Google SRE)
