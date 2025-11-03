# Quick Start Guide

Get up and running with the Traefik GeoBlock plugin in 5 minutes!

## Prerequisites

- Traefik v2.5+ or v3.0+ installed
- Basic understanding of Traefik middleware
- GitHub account (to host the plugin)

## Step 1: Publish the Plugin to GitHub

```bash
# Create a new repository on GitHub named 'traefik-geoblock-plugin'

# Clone and push the plugin
git init
git add .
git commit -m "Initial commit: Traefik GeoBlock plugin"
git branch -M main
git remote add origin https://github.com/YOUR_USERNAME/traefik-geoblock-plugin.git
git push -u origin main

# Create and push a version tag
git tag v0.1.0
git push --tags
```

## Step 2: Configure Traefik

Edit your `traefik.yml` (static configuration):

```yaml
experimental:
  plugins:
    geoblock:
      moduleName: github.com/YOUR_USERNAME/traefik-geoblock-plugin
      version: v0.1.0
```

## Step 3: Create a Middleware

Create or edit your dynamic configuration file (e.g., `dynamic.yml`):

```yaml
http:
  middlewares:
    my-geoblock:
      plugin:
        geoblock:
          allowedCountries:
            - US
            - CA
            - GB
          blockMessage: "Sorry, this service is not available in your country"
```

## Step 4: Apply to a Router

```yaml
http:
  routers:
    my-app:
      rule: "Host(`example.com`)"
      service: my-service
      middlewares:
        - my-geoblock

  services:
    my-service:
      loadBalancer:
        servers:
          - url: "http://localhost:8080"
```

## Step 5: Restart Traefik

```bash
# Docker
docker-compose restart traefik

# Systemd
sudo systemctl restart traefik

# Docker Swarm
docker service update traefik
```

## Verification

Test that it's working:

```bash
# Should work (from allowed country or with VPN)
curl https://example.com

# Should be blocked (if testing from blocked country)
curl https://example.com
# Response: 403 Forbidden - Sorry, this service is not available in your country
```

## Common Use Cases

### Use Case 1: Only Allow US Traffic

```yaml
middlewares:
  us-only:
    plugin:
      geoblock:
        allowedCountries:
          - US
        blockMessage: "US only service"
```

### Use Case 2: Block Specific Countries

```yaml
middlewares:
  block-spam:
    plugin:
      geoblock:
        blockedCountries:
          - CN
          - RU
        defaultAction: allow
```

### Use Case 3: EU Compliance

```yaml
middlewares:
  eu-only:
    plugin:
      geoblock:
        allowedCountries:
          - AT  # Austria
          - BE  # Belgium
          - DE  # Germany
          - FR  # France
          - IT  # Italy
          - ES  # Spain
          # ... add all EU countries
        blockMessage: "Service available only in EU"
```

## Troubleshooting

### Plugin Not Loading

**Problem**: `plugin not found` error

**Solution**:
1. Verify your GitHub repository is public
2. Check the `moduleName` matches your GitHub repository path
3. Ensure you've created and pushed a version tag
4. Wait a few minutes for GitHub to index the release

### All Requests Blocked

**Problem**: All requests return 403

**Solution**:
1. Check `defaultAction` - set to `allow` if using `allowedCountries`
2. Verify country codes are uppercase (US, not us)
3. Check logs with `logBlocked: true`
4. Test from different IPs/locations

### High Latency

**Problem**: Slow response times

**Solution**:
1. Increase `cacheDuration` to 120+ minutes
2. Consider self-hosting a GeoIP database
3. Use a faster GeoIP API service

### Behind Load Balancer

**Problem**: Wrong IP detected

**Solution**: Configure `trustedProxies`:

```yaml
middlewares:
  my-geoblock:
    plugin:
      geoblock:
        allowedCountries:
          - US
        trustedProxies:
          - "10.0.0.0/8"
          - "172.16.0.0/12"
```

## Next Steps

- Check the [README.md](README.md) for full documentation
- See [advanced-example.yml](advanced-example.yml) for complex scenarios
- Configure rate limiting in combination with geoblocking
- Set up monitoring and alerts

## Support

- **Issues**: https://github.com/YOUR_USERNAME/traefik-geoblock-plugin/issues
- **Traefik Docs**: https://doc.traefik.io/traefik/plugins/
- **Traefik Community**: https://community.traefik.io/

---

**Tip**: Start with `logBlocked: true` to see what's happening, then disable it in production to reduce log volume.
