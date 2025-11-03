# Deployment Checklist

Use this checklist to deploy your Traefik GeoBlock plugin step-by-step.

## ☐ Pre-Deployment

- [ ] Have a GitHub account
- [ ] Traefik v2.5+ or v3.0+ installed and running
- [ ] Access to Traefik configuration files
- [ ] (Optional) Test environment to verify before production

## ☐ GitHub Setup

- [ ] Create new GitHub repository named `traefik-geoblock-plugin`
- [ ] Make repository **public** (required for Traefik plugins)
- [ ] Update `go.mod` with your GitHub username:
  ```
  module github.com/YOUR_USERNAME/traefik-geoblock-plugin
  ```

## ☐ Initial Commit

```bash
# ☐ Initialize git repository
git init

# ☐ Add all files
git add .

# ☐ Make initial commit
git commit -m "Initial commit: Traefik GeoBlock plugin"

# ☐ Set main branch
git branch -M main

# ☐ Add remote (replace YOUR_USERNAME)
git remote add origin https://github.com/YOUR_USERNAME/traefik-geoblock-plugin.git

# ☐ Push to GitHub
git push -u origin main

# ☐ Create version tag
git tag v0.1.0

# ☐ Push tag
git push --tags
```

## ☐ Configure Traefik (Static)

- [ ] Locate your Traefik static configuration file (`traefik.yml`)
- [ ] Add plugin configuration (replace YOUR_USERNAME):
  ```yaml
  experimental:
    plugins:
      geoblock:
        moduleName: github.com/YOUR_USERNAME/traefik-geoblock-plugin
        version: v0.1.0
  ```
- [ ] Save the file

## ☐ Create Middleware (Dynamic)

- [ ] Locate or create dynamic configuration file (e.g., `dynamic.yml`)
- [ ] Choose your blocking strategy:
  
  **Option A: Allowlist (only allow specific countries)**
  ```yaml
  http:
    middlewares:
      geoblock-allowlist:
        plugin:
          geoblock:
            allowedCountries:
              - US
              - CA
              - GB
            blockMessage: "Access only from US, CA, GB"
            logBlocked: true
  ```
  
  **Option B: Blocklist (block specific countries)**
  ```yaml
  http:
    middlewares:
      geoblock-blocklist:
        plugin:
          geoblock:
            blockedCountries:
              - CN
              - RU
            defaultAction: allow
            blockMessage: "Access restricted from your country"
            logBlocked: true
  ```

- [ ] Save the file

## ☐ Apply to Router

- [ ] Add middleware to your router:
  ```yaml
  http:
    routers:
      my-router:
        rule: "Host(`example.com`)"
        service: my-service
        middlewares:
          - geoblock-allowlist  # or geoblock-blocklist
  ```
- [ ] Save the file

## ☐ If Behind Proxy/Load Balancer

- [ ] Add trusted proxies to middleware config:
  ```yaml
  trustedProxies:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
    - "192.168.0.0/16"
  ```

## ☐ Restart Traefik

Choose your deployment method:

**Docker Compose:**
```bash
# ☐ Restart Traefik
docker-compose restart traefik

# ☐ Check logs
docker-compose logs -f traefik
```

**Docker:**
```bash
# ☐ Restart container
docker restart traefik

# ☐ Check logs
docker logs -f traefik
```

**Systemd:**
```bash
# ☐ Restart service
sudo systemctl restart traefik

# ☐ Check status
sudo systemctl status traefik

# ☐ Check logs
sudo journalctl -u traefik -f
```

**Docker Swarm:**
```bash
# ☐ Update service
docker service update traefik

# ☐ Check logs
docker service logs -f traefik
```

## ☐ Verify Plugin Loaded

- [ ] Check Traefik logs for plugin loading messages
- [ ] Look for errors related to "geoblock" or plugin loading
- [ ] Access Traefik dashboard (if enabled) and verify middleware appears

## ☐ Test Functionality

### Basic Test
```bash
# ☐ Test from allowed location (should work)
curl https://your-domain.com

# ☐ Expected: Normal response (200 OK)
```

### Test with VPN/Different IP
```bash
# ☐ Use VPN or proxy from blocked country
curl https://your-domain.com

# ☐ Expected: 403 Forbidden with your block message
```

### Test with Header Spoofing (if proxy is configured)
```bash
# ☐ Test with blocked country IP in header
curl -H "X-Forwarded-For: 1.2.4.8" https://your-domain.com

# ☐ Expected: 403 Forbidden (1.2.4.8 is China)
```

## ☐ Monitor Logs

- [ ] Enable logging initially: `logBlocked: true`
- [ ] Watch logs for blocked requests
- [ ] Verify correct country detection
- [ ] Check for any errors or unexpected behavior

Sample log output:
```
[GeoBlock] Blocked request from IP 1.2.3.4 (Country: CN)
```

## ☐ Performance Optimization

- [ ] Monitor API usage (check ipapi.co dashboard if using free tier)
- [ ] Adjust `cacheDuration` if needed:
  - Low traffic: 30-60 minutes
  - Medium traffic: 60-120 minutes
  - High traffic: 120-240 minutes
- [ ] Consider upgrading GeoIP service if hitting rate limits

## ☐ Production Readiness

- [ ] Disable verbose logging: `logBlocked: false` (or remove)
- [ ] Document your configuration for team members
- [ ] Set up monitoring/alerting if available
- [ ] Add to disaster recovery documentation
- [ ] Test fallback behavior (what happens if GeoIP API is down?)

## ☐ Post-Deployment

- [ ] Monitor blocked request rate
- [ ] Gather feedback from legitimate users (false positives?)
- [ ] Fine-tune country lists based on actual traffic
- [ ] Consider adding rate limiting middleware for additional protection
- [ ] Update documentation with any custom configurations

## 🔍 Troubleshooting Quick Reference

| Issue | Quick Fix |
|-------|-----------|
| Plugin not loading | Wait 5 minutes for GitHub to index, verify tag exists |
| All requests blocked | Check `defaultAction`, verify country codes are uppercase |
| Wrong IP detected | Add `trustedProxies` configuration |
| High latency | Increase `cacheDuration` |
| Logs not showing | Set `logBlocked: true` |

## 📞 Need Help?

- Review README.md for detailed documentation
- Check QUICKSTART.md for common scenarios  
- Review Traefik logs for error messages
- Test with `curl -v` to see detailed response headers
- Verify country code with: `curl https://ipapi.co/[IP]/country/`

## ✅ Deployment Complete!

Once all items are checked, your GeoBlock plugin is live and protecting your services!

---

**Pro Tip**: Keep `logBlocked: true` enabled for the first few days to catch any configuration issues, then disable for production.
