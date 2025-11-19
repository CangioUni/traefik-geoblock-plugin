# Traefik GeoBlock Plugin

A Traefik middleware plugin that blocks or allows traffic based on the geographic location (country) of the client's IP address.

## Features

- 🌍 **Country-based filtering**: Allow or block traffic from specific countries
- 🚀 **High performance**: Built-in caching to minimize API calls
- 🔒 **Flexible configuration**: Support for both allowlists and blocklists
- 📊 **Grafana metrics**: Privacy-respecting structured logging for traffic analytics
- 🏢 **Organization tracking**: Log ISP/company information without exposing IPs
- 🔄 **Proxy support**: Handles X-Forwarded-For and X-Real-IP headers
- 🎯 **Default actions**: Configure default behavior for unknown countries
- 💾 **Smart caching**: Configurable cache duration to optimize performance
- 🗑️ **Automatic cleanup**: Built-in log retention and rotation

## Configuration Options

### Basic Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `allowedCountries` | []string | No | [] | List of ISO 3166-1 alpha-2 country codes to allow (e.g., US, GB, DE) |
| `blockedCountries` | []string | No | [] | List of ISO 3166-1 alpha-2 country codes to block |
| `queryURL` | string | No | `https://ipapi.co/{ip}/json/` | GeoIP lookup API URL (use `{ip}` placeholder) |
| `cacheDuration` | int | No | 60 | Cache duration in minutes |
| `defaultAction` | string | No | allow | Default action for unknown countries: `allow` or `block` |
| `blockMessage` | string | No | Access denied from your country | Message shown to blocked users |
| `logBlocked` | bool | No | true | Legacy stdout logging (includes IPs) |
| `trustedProxies` | []string | No | [] | List of trusted proxy IP addresses/ranges |

### Grafana Metrics Options

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `enableMetricsLog` | bool | No | false | Enable Grafana-compatible metrics logging |
| `metricsLogPath` | string | No | `/var/log/traefik-geoblock/metrics.log` | Path for metrics log file |
| `metricsFlushSeconds` | int | No | 60 | How often to flush aggregated metrics to disk |
| `logRetentionDays` | int | No | 14 | Days to retain log files before automatic cleanup |

> **Note**: For detailed integration instructions, see:
> - [GRAFANA-METRICS.md](GRAFANA-METRICS.md) - Grafana Loki integration and dashboards
> - [PROMETHEUS-INTEGRATION.md](PROMETHEUS-INTEGRATION.md) - Prometheus metrics with secure remote scraping

## Installation

### Step 1: Copy repo to Traefik folder

```bash
cd traefik
mkdir plugins
cd plugins
git clone https://github.com/CangioUni/traefik-geoblock-plugin.git
```

### Step 2: Configure Traefik

Add the plugin to your Traefik static configuration (`traefik.yml`):

```yaml
experimental:
  plugins:
    geoblock:
      moduleName: github.com/CangioUni/traefik-geoblock-plugin
      version: v0.1.0
```

Add folder to compose configuration (`docker-compose.yml`):

```yaml
services:
  traefik:
    volumes:
      - /home/user/dockers/traefik/plugins/traefik-geoblock-plugin:/plugins-local/src/github.com/CangioUni/traefik-geoblock-plugin/
```

## Usage Examples

### Example 1: Allow Only Specific Countries

Only allow traffic from IT and DE:

```yaml
http:
  middlewares:
    geoblock-allowlist:
      plugin:
        geoblock:
          allowedCountries:
            - IT
            - DE
          blockMessage: "Access is only available from US, CA, and GB"
          logBlocked: true

  routers:
    my-router:
      rule: "Host(`example.com`)"
      service: my-service
      middlewares:
        - geoblock-allowlist
```

### Example 2: Block Specific Countries

Block traffic from specific countries:

```yaml
http:
  middlewares:
    geoblock-blocklist:
      plugin:
        geoblock:
          blockedCountries:
            - CN
            - RU
            - KP
          defaultAction: allow
          blockMessage: "Access from your region is restricted"
          logBlocked: true
```

### Example 3: Strict Mode (Block All Except Allowed)

Block all countries by default, only allow specific ones:

```yaml
http:
  middlewares:
    geoblock-strict:
      plugin:
        geoblock:
          allowedCountries:
            - US
          defaultAction: block
          blockMessage: "Service not available in your country"
          cacheDuration: 120
```

### Example 4: With Proxy Support

Configure trusted proxies when behind load balancers:

```yaml
http:
  middlewares:
    geoblock-with-proxy:
      plugin:
        geoblock:
          allowedCountries:
            - US
            - CA
          trustedProxies:
            - "10.0.0.0/8"
            - "172.16.0.0/12"
            - "192.168.0.0/16"
          logBlocked: true
```

### Example 5: With Grafana Metrics

Enable privacy-respecting metrics for Grafana dashboards:

```yaml
http:
  middlewares:
    geoblock-with-metrics:
      plugin:
        geoblock:
          blockedCountries:
            - CN
            - RU
          # Enable Grafana-compatible logging
          enableMetricsLog: true
          metricsLogPath: "/var/log/traefik-geoblock/metrics.log"
          metricsFlushSeconds: 60
          logRetentionDays: 14
          # Disable IP logging for privacy
          logBlocked: false
```

See [GRAFANA-METRICS.md](GRAFANA-METRICS.md) for complete setup instructions and dashboard examples.

## How It Works

1. **IP Extraction**: The plugin extracts the client's IP address from the request, checking:
   - `X-Forwarded-For` header (skipping trusted proxies)
   - `X-Real-IP` header
   - Direct connection IP (`RemoteAddr`)

2. **Cache Check**: Checks if the IP's country is already cached

3. **GeoIP Lookup**: If not cached, queries the configured GeoIP API

4. **Decision**: Determines if the request should be blocked based on:
   - Allowlist (if configured, only these countries are allowed)
   - Blocklist (if configured, these countries are blocked)
   - Default action (for unknown countries)

5. **Response**: Either blocks the request (403 Forbidden) or passes it through

## GeoIP Services

The plugin supports any GeoIP service that returns JSON with a `country_code` field. The default service is ipapi.co, which offers:

- **Free tier**: 30,000 requests per month
- **No API key required**
- **Response format**: JSON with country_code field

### Alternative Services

You can use other services by changing the `databaseURL`:

```yaml
# ip-api.com (100 requests/minute free)
databaseURL: "http://ip-api.com/json/{ip}"

# ipinfo.io (requires API token)
databaseURL: "https://ipinfo.io/{ip}/json/?token=YOUR_TOKEN"

# ipgeolocation.io (requires API key)
databaseURL: "https://api.ipgeolocation.io/ipgeo?apiKey=YOUR_KEY&ip={ip}"
```


## Performance Considerations

- **Caching**: Set an appropriate `cacheDuration` based on your traffic patterns
- **API Rate Limits**: Monitor your GeoIP service usage
- **Private IPs**: The plugin automatically allows private IP ranges (useful for development)

## Country Codes

Use ISO 3166-1 alpha-2 country codes. Common examples:

| Country | Code | Country | Code |
|---------|------|---------|------|
| United States | US | Germany | DE |
| Canada | CA | France | FR |
| United Kingdom | GB | Spain | ES |
| Australia | AU | Italy | IT |
| Japan | JP | Brazil | BR |
| China | CN | India | IN |
| Russia | RU | Mexico | MX |

Full list: https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2

## Testing

### Local Testing with Docker Compose

1. Start the services:
```bash
docker-compose up -d
```

2. Test the service:
```bash
curl http://whoami.localhost
```

### Testing with Different IPs

You can test with specific IPs by modifying headers:

```bash
# Test with a US IP
curl -H "X-Forwarded-For: 8.8.8.8" http://whoami.localhost

# Test with a CN IP
curl -H "X-Forwarded-For: 1.2.4.8" http://whoami.localhost
```

## Troubleshooting

### Plugin Not Loading

- Ensure the plugin is properly tagged in GitHub
- Check Traefik logs for plugin loading errors
- Verify the `moduleName` matches your repository path

### Requests Not Being Blocked

- Enable `logBlocked: true` to see what's happening
- Check if IP is being correctly extracted (review logs)
- Verify country codes are uppercase (e.g., `US` not `us`)

### High Latency

- Increase `cacheDuration` to reduce API calls
- Consider self-hosting a GeoIP database (like MaxMind GeoLite2)
- Check GeoIP service response times

## Security Considerations

- **Spoofing**: Configure `trustedProxies` properly to prevent IP spoofing
- **Private IPs**: The plugin automatically allows private IPs (development friendly)
- **API Limits**: Monitor your GeoIP service usage to avoid rate limiting
- **Caching**: Longer cache durations reduce API calls but may miss IP relocations

## License

MIT License - feel free to use and modify as needed.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Support

For issues and questions, please use the GitHub issue tracker.
