# Grafana Metrics Logging

This document explains the Grafana-compatible metrics logging feature for the Traefik GeoBlock plugin.

> **Note**: If you need to expose metrics to Prometheus with secure remote scraping, IP restrictions, and authentication, see [PROMETHEUS-INTEGRATION.md](PROMETHEUS-INTEGRATION.md).

## Overview

The metrics logging feature provides structured, privacy-respecting logs that can be ingested by Grafana Loki or other log aggregation systems. It tracks the number of hits from each country and organization without logging individual IP addresses.

## Features

- **Privacy-focused**: No IP addresses are logged, only country codes and organization information
- **Aggregated metrics**: Counts are aggregated over configurable time windows (default: 60 seconds)
- **Structured JSON logs**: Each log entry is a JSON object for easy parsing
- **Automatic retention**: Old logs are automatically cleaned up based on configured retention period
- **Dual tracking**: Tracks both allowed and blocked requests separately
- **Organization data**: Includes ISP/organization information when available from GeoIP APIs

## Log Format

Each log entry is a single-line JSON object with the following structure:

```json
{
  "timestamp": "2025-11-10T12:34:56Z",
  "country": "US",
  "organization": "Google LLC",
  "action": "blocked",
  "count": 15
}
```

### Fields

- **timestamp**: ISO 8601 timestamp in UTC
- **country**: Two-letter ISO country code (e.g., "US", "GB", "CN")
- **organization**: ISP or organization name (optional, may be empty)
- **action**: Either "allowed" or "blocked"
- **count**: Number of requests in this time window

## Configuration

Add these configuration options to your Traefik middleware:

```yaml
http:
  middlewares:
    my-geoblock:
      plugin:
        traefik-geoblock-plugin:
          # Enable metrics logging
          enableMetricsLog: true

          # Log file path (ensure directory exists and is writable)
          metricsLogPath: "/var/log/traefik-geoblock/metrics.log"

          # Flush interval in seconds (default: 60)
          # Lower = more real-time, higher = more aggregation
          metricsFlushSeconds: 60

          # Retention period in days (default: 14)
          logRetentionDays: 14

          # Disable legacy IP logging for privacy
          logBlocked: false
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enableMetricsLog` | bool | `false` | Enable Grafana-compatible metrics logging |
| `metricsLogPath` | string | `/var/log/traefik-geoblock/metrics.log` | Path to the metrics log file |
| `metricsFlushSeconds` | int | `60` | How often to flush aggregated metrics to disk |
| `logRetentionDays` | int | `14` | Number of days to retain log files |
| `logBlocked` | bool | `true` | Legacy stdout logging (includes IPs - disable for privacy) |

## Setup Instructions

### 1. Create Log Directory

```bash
sudo mkdir -p /var/log/traefik-geoblock
sudo chown traefik:traefik /var/log/traefik-geoblock
sudo chmod 755 /var/log/traefik-geoblock
```

### 2. Configure Traefik

See `grafana-metrics-example.yml` for a complete configuration example.

### 3. Restart Traefik

```bash
sudo systemctl restart traefik
```

### 4. Verify Logging

Check that metrics are being written:

```bash
tail -f /var/log/traefik-geoblock/metrics.log
```

You should see JSON lines like:

```json
{"timestamp":"2025-11-10T12:34:56Z","country":"US","organization":"Google LLC","action":"allowed","count":42}
{"timestamp":"2025-11-10T12:34:56Z","country":"CN","organization":"China Telecom","action":"blocked","count":7}
```

## Grafana Integration

### Option 1: Grafana Loki

Loki is designed for log aggregation and works perfectly with this structured JSON format.

#### Install Promtail

Promtail ships logs to Loki. Install it on the same machine as Traefik:

```bash
# Download Promtail
wget https://github.com/grafana/loki/releases/download/v2.9.3/promtail-linux-amd64.zip
unzip promtail-linux-amd64.zip
sudo mv promtail-linux-amd64 /usr/local/bin/promtail
sudo chmod +x /usr/local/bin/promtail
```

#### Configure Promtail

Create `/etc/promtail/config.yml`:

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://localhost:3100/loki/api/v1/push

scrape_configs:
  - job_name: geoblock-metrics
    static_configs:
      - targets:
          - localhost
        labels:
          job: geoblock
          __path__: /var/log/traefik-geoblock/metrics.log
    pipeline_stages:
      - json:
          expressions:
            timestamp: timestamp
            country: country
            organization: organization
            action: action
            count: count
      - labels:
          country:
          action:
          organization:
      - timestamp:
          source: timestamp
          format: RFC3339
```

#### Start Promtail

```bash
sudo promtail -config.file=/etc/promtail/config.yml
```

### Option 2: Direct File Ingestion

If you're using Grafana with a data source that can read local files, you can query the logs directly.

## Grafana Dashboard Queries

### Blocked Requests by Country (Last 24h)

```logql
sum by (country) (
  count_over_time({job="geoblock", action="blocked"}[24h])
)
```

### Allowed Requests by Country (Last 24h)

```logql
sum by (country) (
  count_over_time({job="geoblock", action="allowed"}[24h])
)
```

### Top Organizations (Blocked)

```logql
sum by (organization) (
  count_over_time({job="geoblock", action="blocked"}[24h])
) | sort desc | limit 10
```

### Total Request Counts

```logql
sum(count_over_time({job="geoblock"}[24h]))
```

### Blocked vs Allowed Ratio

```logql
sum by (action) (
  count_over_time({job="geoblock"}[24h])
)
```

## Example Grafana Dashboard

Create a dashboard with these panels:

1. **World Map**: Show blocked requests by country
   - Visualization: Geomap
   - Query: Blocked requests by country
   - Color by: count value

2. **Time Series**: Requests over time
   - Visualization: Time series
   - Query: Requests grouped by action
   - Show both allowed and blocked lines

3. **Bar Chart**: Top blocked countries
   - Visualization: Bar chart
   - Query: Top 10 countries by blocked count
   - Sort: Descending

4. **Table**: Recent activity
   - Visualization: Table
   - Query: Last 100 log entries
   - Columns: timestamp, country, organization, action, count

5. **Stat Panel**: Total blocked requests
   - Visualization: Stat
   - Query: Sum of all blocked requests
   - Color: Red

## Privacy Considerations

This metrics system is designed with privacy in mind:

1. **No IP logging**: Individual IP addresses are never written to the metrics log
2. **Aggregation**: Requests are aggregated over time windows, not tracked individually
3. **Minimal PII**: Only country codes and organization names are logged
4. **Automatic cleanup**: Old logs are automatically deleted after the retention period

## Performance

The metrics system is designed to be lightweight:

- **In-memory aggregation**: Metrics are aggregated in memory before flushing
- **Configurable flush interval**: Balance between real-time data and disk I/O
- **Efficient JSON encoding**: Single-line JSON for fast parsing
- **Automatic cleanup**: Old logs are cleaned up to prevent disk space issues

## Troubleshooting

### Logs not appearing

1. Check that the log directory exists and is writable:
   ```bash
   ls -la /var/log/traefik-geoblock/
   ```

2. Check Traefik logs for errors:
   ```bash
   journalctl -u traefik -f
   ```

3. Verify the configuration is loaded:
   ```bash
   grep -A 20 "geoblock" /etc/traefik/traefik.yml
   ```

### Empty organization field

Some GeoIP APIs don't provide organization information in their free tiers. Consider:

- Using ipinfo.io with a paid plan
- Using ip-api.com which includes ISP info for free
- Accepting that organization may be empty for some requests

### High disk usage

1. Reduce the retention period:
   ```yaml
   logRetentionDays: 7  # Instead of 14
   ```

2. Increase the flush interval:
   ```yaml
   metricsFlushSeconds: 300  # 5 minutes instead of 1
   ```

3. Implement log rotation with logrotate

## Log Rotation (Optional)

While the plugin includes automatic cleanup, you can also use logrotate:

Create `/etc/logrotate.d/traefik-geoblock`:

```
/var/log/traefik-geoblock/metrics.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    create 0644 traefik traefik
    postrotate
        systemctl reload traefik > /dev/null 2>&1 || true
    endscript
}
```

## Prometheus Integration

If you prefer to use Prometheus instead of (or in addition to) Grafana Loki, see the comprehensive **[Prometheus Integration Guide](PROMETHEUS-INTEGRATION.md)** which covers:

- Converting JSON logs to Prometheus metrics
- Exposing metrics through Traefik with a secure endpoint
- Restricting access to specific IP addresses
- Adding authentication for remote scraping
- Complete configuration examples
- Security best practices

The Prometheus guide is ideal for scenarios where you need:
- Remote metrics scraping from a dedicated Prometheus server
- Strong access controls with IP whitelisting and authentication
- Integration with existing Prometheus + Grafana monitoring stacks
- Long-term metrics storage and alerting capabilities

## Support

For issues or questions about the metrics logging feature:

1. Check this documentation
2. Review the example configuration in `grafana-metrics-example.yml`
3. Open an issue on GitHub with:
   - Your configuration
   - Traefik logs
   - Example metric log entries
   - Grafana version and data source type
