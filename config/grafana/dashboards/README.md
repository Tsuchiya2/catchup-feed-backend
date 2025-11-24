# Grafana Dashboards

This directory contains Grafana dashboard JSON files for monitoring CatchUp Feed application metrics.

## Available Dashboards

### Email Notifications Dashboard

**File**: `email-notifications.json`
**UID**: `email-notifications`
**Tags**: email, notifications, catchup-feed

Visualizes email notification system metrics collected via Prometheus.

#### Panels

1. **Email Send Success Rate** (Gauge)
   - Shows the percentage of successfully sent emails over the last hour
   - Green: >95%, Yellow: 90-95%, Red: <90%
   - Query: `(rate(notification_sent_total{channel="email",status="success"}[1h]) / rate(notification_sent_total{channel="email"}[1h])) * 100`

2. **Total Emails Sent** (Stat)
   - Counter showing total number of successfully sent emails
   - Includes sparkline graph
   - Query: `notification_sent_total{channel="email",status="success"}`

3. **Email Failures** (Stat)
   - Counter showing total number of failed email sends
   - Red color indicator when failures occur
   - Query: `notification_sent_total{channel="email",status="failure"}`

4. **Email Send Duration** (Time Series Graph)
   - Line graph showing email send latency percentiles (p50, p95, p99)
   - Unit: milliseconds
   - Queries:
     - p50: `histogram_quantile(0.50, rate(notification_duration_seconds_bucket{channel="email"}[5m])) * 1000`
     - p95: `histogram_quantile(0.95, rate(notification_duration_seconds_bucket{channel="email"}[5m])) * 1000`
     - p99: `histogram_quantile(0.99, rate(notification_duration_seconds_bucket{channel="email"}[5m])) * 1000`

5. **Rate Limit Usage** (Gauge)
   - Shows email rate limit usage as percentage
   - Two metrics: hourly and daily
   - Green: <70%, Yellow: 70-90%, Red: >90%
   - Note: Calculations are estimates based on rate limit hits
   - Queries:
     - Hourly: `(rate(notification_rate_limit_hits_total{channel="email"}[1h]) / 10) * 100`
     - Daily: `(rate(notification_rate_limit_hits_total{channel="email"}[24h]) / 100) * 100`

6. **Fallback Events** (Stat)
   - Shows dropped email notifications by reason
   - Reasons: pool_full, circuit_open, disabled
   - Red indicator when events occur
   - Query: `sum by (reason) (notification_dropped_total{channel="email"})`

7. **Emails by Priority** (Pie Chart)
   - Distribution of emails by priority level (high, normal, low)
   - Shows percentage and count in legend
   - Query: `sum by (priority) (notification_sent_total{channel="email"})`
   - Note: Priority label may not be available in current implementation

#### Dashboard Configuration

- **Time Range**: Last 24 hours (default)
- **Refresh Rate**: 30 seconds
- **Theme**: Dark
- **Timezone**: Browser default

## Installation

### Prerequisites

- Grafana v9.0 or higher
- Prometheus data source configured in Grafana with name "Prometheus"
- CatchUp Feed application with Prometheus metrics enabled

### Import Steps

1. Open Grafana web interface
2. Navigate to **Dashboards** → **Import**
3. Click **Upload JSON file**
4. Select `email-notifications.json` from this directory
5. Configure the following:
   - **Prometheus data source**: Select your Prometheus instance
   - **Folder**: Choose destination folder (optional)
6. Click **Import**

### Alternative Import Method

1. Open Grafana web interface
2. Navigate to **Dashboards** → **Import**
3. Copy the entire contents of `email-notifications.json`
4. Paste into the **Import via panel json** text box
5. Click **Load**
6. Configure data source and folder
7. Click **Import**

## Prometheus Metrics

The dashboard uses the following Prometheus metrics from the CatchUp Feed application:

- `notification_sent_total{channel="email", status="success|failure"}` - Total emails sent by status
- `notification_duration_seconds_bucket{channel="email"}` - Email send duration histogram
- `notification_rate_limit_hits_total{channel="email"}` - Rate limit hit counter
- `notification_dropped_total{channel="email", reason="..."}` - Dropped emails counter

These metrics are implemented in:
- **Code**: `internal/usecase/notify/metrics.go`
- **Endpoint**: `/metrics` (Prometheus scrape endpoint)

## Troubleshooting

### No data displayed

1. Verify Prometheus is scraping metrics:
   ```bash
   curl http://localhost:9090/api/v1/targets
   ```

2. Check if email metrics exist in Prometheus:
   ```promql
   notification_sent_total{channel="email"}
   ```

3. Verify CatchUp Feed application is exposing metrics:
   ```bash
   curl http://localhost:8080/metrics
   ```

### Incorrect data source

If the dashboard shows "Data source not found":

1. Edit dashboard settings
2. Go to **Variables** (or **Settings** → **Variables**)
3. Update the data source to match your Prometheus instance name
4. Save dashboard

### Missing panels or queries fail

1. Verify Prometheus metric names match the implementation
2. Check that `channel="email"` label exists in your metrics
3. Ensure sufficient time range for data collection

## Customization

### Changing refresh rate

1. Click the refresh dropdown in the top right
2. Select desired interval (10s, 30s, 1m, 5m, etc.)
3. Save dashboard to persist change

### Adjusting time range

1. Click the time range picker in the top right
2. Select preset range or custom range
3. Save dashboard to persist change

### Modifying thresholds

To adjust alert thresholds (e.g., success rate):

1. Edit panel (click panel title → Edit)
2. Go to **Field** tab on the right
3. Scroll to **Thresholds**
4. Modify values and colors
5. Apply changes
6. Save dashboard

### Adding new panels

1. Click **Add panel** button
2. Select **Add a new panel**
3. Configure query and visualization
4. Add panel to dashboard
5. Save dashboard

## Maintenance

### Updating dashboard

To update the dashboard JSON file:

1. Make changes in Grafana UI
2. Click **Dashboard settings** (gear icon)
3. Go to **JSON Model**
4. Copy the JSON
5. Update `email-notifications.json` in this directory
6. Commit changes to version control

### Exporting dashboard

To export for backup or sharing:

1. Open dashboard
2. Click **Share** icon
3. Go to **Export** tab
4. Click **Save to file**
5. JSON file will be downloaded

## Support

For issues or questions:
- Check Prometheus metrics implementation: `internal/usecase/notify/metrics.go`
- Review Grafana documentation: https://grafana.com/docs/
- Review Prometheus query documentation: https://prometheus.io/docs/prometheus/latest/querying/basics/
