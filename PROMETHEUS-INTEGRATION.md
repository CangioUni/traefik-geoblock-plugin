# Prometheus Integration Guide

This guide explains how to expose GeoBlock plugin metrics to Prometheus and configure secure remote scraping with IP restrictions and authentication.

## Overview

The Traefik GeoBlock plugin logs structured metrics to JSON files. To make these metrics available to Prometheus, we'll:

1. Convert JSON logs to Prometheus metrics using a metrics exporter
2. Expose the metrics endpoint through Traefik
3. Restrict access to a specific IP address (your Prometheus server)
4. Add authentication for additional security

## Architecture

```
┌─────────────────┐
│  GeoBlock       │
│  Plugin         │ ──> JSON logs ──> ┌──────────────────┐
└─────────────────┘                   │ Prometheus       │
                                      │ json_exporter    │
                                      │ :7979/metrics    │
                                      └──────────────────┘
                                             │
                                             │ HTTP
                                             ▼
                                      ┌──────────────────┐
                                      │ Traefik          │
                                      │ /geoblock-metrics│
                                      │ + IP whitelist   │
                                      │ + Basic Auth     │
                                      └──────────────────┘
                                             │
                                             │ HTTPS
                                             ▼
                                      ┌──────────────────┐
                                      │ Remote Prometheus│
                                      │ Server           │
                                      │ (Scraper)        │
                                      └──────────────────┘
```

## Step 1: Enable Metrics Logging

First, configure the GeoBlock plugin to log metrics in your Traefik dynamic configuration:

```yaml
http:
  middlewares:
    geoblock-with-metrics:
      plugin:
        traefik-geoblock-plugin:
          # Basic configuration
          blockedCountries:
            - CN
            - RU
            - KP

          # Enable metrics logging
          enableMetricsLog: true
          metricsLogPath: "/var/log/traefik-geoblock/metrics.log"
          metricsFlushSeconds: 60
          logRetentionDays: 14
```

## Step 2: Install Prometheus JSON Exporter

The [Prometheus JSON Exporter](https://github.com/prometheus-community/json_exporter) reads JSON logs and exposes them as Prometheus metrics.

### Install the Exporter

```bash
# Download the latest release
wget https://github.com/prometheus-community/json_exporter/releases/download/v0.6.0/json_exporter-0.6.0.linux-amd64.tar.gz

# Extract
tar xzf json_exporter-0.6.0.linux-amd64.tar.gz
cd json_exporter-0.6.0.linux-amd64

# Move to system path
sudo mv json_exporter /usr/local/bin/
sudo chmod +x /usr/local/bin/json_exporter
```

### Configure the Exporter

Create `/etc/json_exporter/config.yml`:

```yaml
---
metrics:
- name: geoblock_requests_total
  type: object
  help: Total number of requests by country and action
  path: '{}'
  labels:
    country: '{.country}'
    organization: '{.organization}'
    action: '{.action}'
  values:
    count: '{.count}'

- name: geoblock_last_update
  type: object
  help: Timestamp of last metric update
  path: '{}'
  values:
    timestamp: '{.timestamp}'
```

### Create Systemd Service

Create `/etc/systemd/system/json_exporter.service`:

```ini
[Unit]
Description=Prometheus JSON Exporter for GeoBlock Metrics
After=network.target

[Service]
Type=simple
User=nobody
Group=nogroup
ExecStart=/usr/local/bin/json_exporter \
  --config.file=/etc/json_exporter/config.yml \
  --web.listen-address=127.0.0.1:7979
Restart=on-failure
RestartSec=5s

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/traefik-geoblock

[Install]
WantedBy=multi-user.target
```

### Start the Service

```bash
sudo systemctl daemon-reload
sudo systemctl enable json_exporter
sudo systemctl start json_exporter

# Check status
sudo systemctl status json_exporter

# Test locally
curl http://127.0.0.1:7979/metrics
```

## Step 3: Expose Metrics Through Traefik

Now we'll expose the metrics endpoint through Traefik with IP restrictions and authentication.

### Generate Authentication Credentials

First, create a password hash for Basic Authentication:

```bash
# Install htpasswd if needed
sudo apt-get install apache2-utils

# Generate password (replace 'your-password' with a strong password)
htpasswd -nb prometheus your-password

# Output will be like:
# prometheus:$apr1$rPsXN5pD$abc123xyz...
```

Save this output - you'll need it for the Traefik configuration.

### Create Traefik Dynamic Configuration

Create or update your Traefik dynamic configuration file (e.g., `/etc/traefik/dynamic/prometheus-metrics.yml`):

```yaml
http:
  # Routers
  routers:
    geoblock-metrics:
      # Match the metrics endpoint
      rule: "Host(`your-domain.com`) && PathPrefix(`/geoblock-metrics`)"

      # Use HTTPS (assuming you have cert resolver configured)
      entryPoints:
        - websecure

      # Apply middlewares for security
      middlewares:
        - geoblock-metrics-stripprefix
        - geoblock-metrics-ipwhitelist
        - geoblock-metrics-auth

      # Point to the json_exporter service
      service: geoblock-metrics-service

      # TLS configuration (optional)
      tls:
        certResolver: letsencrypt

  # Middlewares
  middlewares:
    # Strip the /geoblock-metrics prefix before forwarding
    geoblock-metrics-stripprefix:
      stripPrefix:
        prefixes:
          - "/geoblock-metrics"

    # IP Whitelist - ONLY allow your Prometheus server
    geoblock-metrics-ipwhitelist:
      ipWhiteList:
        sourceRange:
          - "192.168.1.100/32"  # Replace with your Prometheus server IP
          # Add multiple IPs if needed:
          # - "10.0.0.50/32"

    # Basic Authentication
    geoblock-metrics-auth:
      basicAuth:
        users:
          # Replace with your htpasswd output from above
          - "prometheus:$apr1$rPsXN5pD$abc123xyz..."

        # Optional: Custom realm
        realm: "Prometheus Metrics"

  # Services
  services:
    geoblock-metrics-service:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:7979"
```

### Restart Traefik

```bash
sudo systemctl restart traefik

# Check logs
sudo journalctl -u traefik -f
```

## Step 4: Configure Prometheus Scraping

On your Prometheus server, add the scrape configuration to `/etc/prometheus/prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'traefik-geoblock'

    # Scrape interval
    scrape_interval: 60s
    scrape_timeout: 10s

    # Use HTTPS
    scheme: https

    # Basic authentication
    basic_auth:
      username: prometheus
      password: your-password  # The password you set earlier

    # TLS configuration (if using self-signed certificates)
    tls_config:
      # For Let's Encrypt or valid certs, you can omit this
      # For self-signed certs:
      # insecure_skip_verify: true
      # Or provide CA:
      # ca_file: /path/to/ca.crt

    # Static targets
    static_configs:
      - targets:
          - 'your-domain.com:443'
        labels:
          instance: 'traefik-geoblock-main'
          environment: 'production'

    # Metrics path
    metrics_path: '/geoblock-metrics/metrics'

    # Honor labels from the exporter
    honor_labels: true
```

### Reload Prometheus

```bash
# Validate configuration
promtool check config /etc/prometheus/prometheus.yml

# Reload Prometheus
sudo systemctl reload prometheus

# Or send SIGHUP
sudo killall -HUP prometheus
```

## Step 5: Verify the Setup

### Test from Local Machine

```bash
# Test without auth (should fail)
curl https://your-domain.com/geoblock-metrics/metrics

# Test with auth (should succeed)
curl -u prometheus:your-password https://your-domain.com/geoblock-metrics/metrics

# Test from different IP (should fail with 403 Forbidden)
# Use a VPN or different server
```

### Check Prometheus Targets

1. Open Prometheus UI: `http://prometheus-server:9090`
2. Navigate to **Status → Targets**
3. Find the `traefik-geoblock` job
4. Verify the status is **UP** and shows the last successful scrape

### Query Metrics

In Prometheus, try these queries:

```promql
# Total requests by country
sum by (country) (geoblock_requests_total)

# Blocked requests by country
sum by (country) (geoblock_requests_total{action="blocked"})

# Allowed requests by country
sum by (country) (geoblock_requests_total{action="allowed"})

# Top organizations with blocked requests
topk(10, sum by (organization) (geoblock_requests_total{action="blocked"}))

# Rate of blocked requests per minute
rate(geoblock_requests_total{action="blocked"}[5m])
```

## Alternative: File-based Metrics Collection

If you prefer, Prometheus can also scrape metrics directly from text files using the [Node Exporter's Textfile Collector](https://github.com/prometheus/node_exporter#textfile-collector).

### Setup Node Exporter with Textfile Collector

```bash
# Install node_exporter
wget https://github.com/prometheus/node_exporter/releases/download/v1.7.0/node_exporter-1.7.0.linux-amd64.tar.gz
tar xzf node_exporter-1.7.0.linux-amd64.tar.gz
sudo mv node_exporter-1.7.0.linux-amd64/node_exporter /usr/local/bin/

# Create textfile directory
sudo mkdir -p /var/lib/node_exporter/textfile_collector

# Create systemd service
sudo tee /etc/systemd/system/node_exporter.service > /dev/null <<EOF
[Unit]
Description=Node Exporter
After=network.target

[Service]
Type=simple
User=nobody
ExecStart=/usr/local/bin/node_exporter \\
  --collector.textfile.directory=/var/lib/node_exporter/textfile_collector \\
  --web.listen-address=127.0.0.1:9100
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable node_exporter
sudo systemctl start node_exporter
```

### Create Conversion Script

Create a script to convert JSON logs to Prometheus format at `/usr/local/bin/geoblock_to_prometheus.sh`:

```bash
#!/bin/bash

# Read the metrics log and convert to Prometheus format
METRICS_LOG="/var/log/traefik-geoblock/metrics.log"
OUTPUT_FILE="/var/lib/node_exporter/textfile_collector/geoblock.prom"
TEMP_FILE="${OUTPUT_FILE}.$$"

# Start with help text
cat > "$TEMP_FILE" <<EOF
# HELP geoblock_requests_total Total number of requests by country and action
# TYPE geoblock_requests_total gauge
EOF

# Parse JSON logs and create metrics
if [ -f "$METRICS_LOG" ]; then
  tail -n 1000 "$METRICS_LOG" | jq -r '
    "geoblock_requests_total{country=\"\(.country)\",organization=\"\(.organization // "")\",action=\"\(.action)\"} \(.count)"
  ' >> "$TEMP_FILE" 2>/dev/null
fi

# Atomically move to final location
mv "$TEMP_FILE" "$OUTPUT_FILE"
```

Make it executable and run it periodically:

```bash
sudo chmod +x /usr/local/bin/geoblock_to_prometheus.sh

# Add to crontab (run every minute)
echo "* * * * * /usr/local/bin/geoblock_to_prometheus.sh" | sudo crontab -
```

Then expose Node Exporter through Traefik using the same IP whitelist and authentication setup.

## Security Best Practices

### 1. IP Whitelisting

- **Specific IPs Only**: Always use `/32` CIDR notation for single IPs
- **Multiple Locations**: If Prometheus is behind a load balancer, whitelist all possible source IPs
- **Cloud Environments**: Consider using security groups or firewall rules in addition to Traefik

```yaml
# Good: Specific IP
sourceRange:
  - "192.168.1.100/32"

# Bad: Too broad
sourceRange:
  - "0.0.0.0/0"
```

### 2. Strong Authentication

- **Strong Passwords**: Use long, randomly generated passwords
- **Rotate Regularly**: Change credentials periodically
- **Consider mTLS**: For even stronger security, use mutual TLS authentication

Generate strong passwords:
```bash
# Generate a 32-character random password
openssl rand -base64 32
```

### 3. HTTPS Only

- **Always use TLS**: Never expose metrics over plain HTTP
- **Valid Certificates**: Use Let's Encrypt or proper CA-signed certificates
- **TLS 1.2+**: Ensure only modern TLS versions are enabled

### 4. Network Segmentation

- **Private Networks**: If possible, keep metrics endpoints on private networks
- **VPN**: Consider requiring VPN access for metrics scraping
- **Firewall Rules**: Add OS-level firewall rules as an additional layer

```bash
# Example: UFW firewall rule
sudo ufw allow from 192.168.1.100 to any port 443 proto tcp comment 'Prometheus scraper'
```

### 5. Monitoring and Alerting

- **Failed Auth Attempts**: Monitor Traefik logs for failed authentication attempts
- **Unusual Access Patterns**: Alert on scrapes from unexpected IPs
- **Rate Limiting**: Consider adding rate limiting middleware

Example rate limiting:
```yaml
middlewares:
  geoblock-metrics-ratelimit:
    rateLimit:
      average: 10
      burst: 20
      period: 1m
```

## Troubleshooting

### Metrics Endpoint Returns 403 Forbidden

**Cause**: Your IP is not whitelisted

**Solution**:
1. Verify your Prometheus server's public IP:
   ```bash
   curl ifconfig.me
   ```
2. Update the IP whitelist in Traefik configuration
3. Reload Traefik configuration

### Metrics Endpoint Returns 401 Unauthorized

**Cause**: Incorrect credentials

**Solution**:
1. Verify the htpasswd hash is correct
2. Ensure the password in Prometheus config matches
3. Check for special characters that need escaping

### Prometheus Shows Target as DOWN

**Cause**: Multiple possible issues

**Solution**:
```bash
# Test connectivity from Prometheus server
curl -v -u prometheus:your-password https://your-domain.com/geoblock-metrics/metrics

# Check Traefik logs
sudo journalctl -u traefik -f | grep geoblock

# Check json_exporter logs
sudo journalctl -u json_exporter -f

# Verify json_exporter is running
sudo systemctl status json_exporter
```

### No Metrics Data Showing

**Cause**: GeoBlock plugin not logging or exporter not reading logs

**Solution**:
```bash
# Verify metrics log exists and has data
tail -f /var/log/traefik-geoblock/metrics.log

# Check log file permissions
ls -la /var/log/traefik-geoblock/

# Test json_exporter directly
curl http://127.0.0.1:7979/metrics

# Verify GeoBlock configuration
grep -A 20 "geoblock" /etc/traefik/traefik.yml
```

### SSL Certificate Errors

**Cause**: Certificate issues or mismatched domains

**Solution**:
```yaml
# In Prometheus config, temporarily skip verification (testing only!)
tls_config:
  insecure_skip_verify: true

# Check certificate
openssl s_client -connect your-domain.com:443 -servername your-domain.com

# Verify Let's Encrypt certificate
sudo certbot certificates
```

## Example Grafana Dashboard

Once metrics are in Prometheus, create a Grafana dashboard:

### Panel 1: Requests by Country (Map)

**Query**:
```promql
sum by (country) (geoblock_requests_total)
```

**Visualization**: Geomap

### Panel 2: Blocked vs Allowed (Time Series)

**Query**:
```promql
sum by (action) (rate(geoblock_requests_total[5m]))
```

**Visualization**: Time series

### Panel 3: Top Blocked Countries (Bar Chart)

**Query**:
```promql
topk(10, sum by (country) (geoblock_requests_total{action="blocked"}))
```

**Visualization**: Bar chart (horizontal)

### Panel 4: Total Blocked Requests (Stat)

**Query**:
```promql
sum(geoblock_requests_total{action="blocked"})
```

**Visualization**: Stat panel (with red threshold)

## Complete Example Configuration

Here's a complete working example combining all components:

```yaml
# /etc/traefik/dynamic/geoblock-prometheus.yml
http:
  routers:
    geoblock-prometheus:
      rule: "Host(`metrics.example.com`) && PathPrefix(`/geoblock`)"
      entryPoints:
        - websecure
      middlewares:
        - geoblock-strip
        - geoblock-ipfilter
        - geoblock-auth
        - geoblock-ratelimit
      service: geoblock-exporter
      tls:
        certResolver: letsencrypt

  middlewares:
    geoblock-strip:
      stripPrefix:
        prefixes:
          - "/geoblock"

    geoblock-ipfilter:
      ipWhiteList:
        sourceRange:
          - "203.0.113.50/32"  # Prometheus server

    geoblock-auth:
      basicAuth:
        users:
          - "prometheus:$apr1$H6uskkkW$IgXLP6ewTrSuBkTrqE8wj/"

    geoblock-ratelimit:
      rateLimit:
        average: 10
        period: 1m
        burst: 20

  services:
    geoblock-exporter:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:7979"
```

```yaml
# /etc/prometheus/prometheus.yml - scrape config
scrape_configs:
  - job_name: 'geoblock'
    scrape_interval: 60s
    scheme: https
    basic_auth:
      username: prometheus
      password: secure-password-here
    metrics_path: '/geoblock/metrics'
    static_configs:
      - targets: ['metrics.example.com:443']
        labels:
          service: 'geoblock'
```

## Summary

You now have a complete, secure setup for exposing GeoBlock metrics to Prometheus:

✅ **Metrics Logging**: GeoBlock plugin logs to JSON files
✅ **Prometheus Export**: JSON exporter converts logs to Prometheus format
✅ **Traefik Exposure**: Metrics endpoint exposed through Traefik
✅ **IP Restriction**: Only your Prometheus server can access
✅ **Authentication**: Basic auth adds an extra security layer
✅ **HTTPS**: All traffic is encrypted

This setup ensures your metrics are both accessible for monitoring and secure from unauthorized access.

## Additional Resources

- [Prometheus JSON Exporter](https://github.com/prometheus-community/json_exporter)
- [Traefik Middlewares Documentation](https://doc.traefik.io/traefik/middlewares/overview/)
- [Prometheus Configuration](https://prometheus.io/docs/prometheus/latest/configuration/configuration/)
- [Grafana Dashboards](https://grafana.com/docs/grafana/latest/dashboards/)
- [GeoBlock Grafana Metrics Guide](GRAFANA-METRICS.md)
