---
title: SaaS 部署指南
description: 以 SaaS 模式部署 Arkloop：网关安全加固、TLS 终止、CI/CD 流水线集成与多实例扩展。
---

# SaaS Deployment Guide

This guide covers deploying Arkloop in SaaS mode (`--mode saas`), which is intended for the official Arkloop SaaS service. SaaS mode differs from standard self-hosted deployment in several ways:

| Dimension | Self-hosted | SaaS |
|-----------|-------------|------|
| Authentication | Local registration + bootstrap admin | OAuth / SSO integration |
| Billing | No credit system | Credit deduction enabled |
| Models | User-configured API keys | Platform-managed providers |
| Domain | localhost / internal network | Public internet + TLS |
| Upgrade | Manual `setup.sh upgrade` | CI/CD pipeline auto-deploy |
| Scale | Single machine | Multi-instance + PGBouncer + S3 |

## Quick Start

```bash
./setup.sh install --mode saas --profile standard --non-interactive --prod
```

SaaS mode automatically enables:
- **PGBouncer** — Connection pooling for high concurrency
- **SeaweedFS** — S3-compatible object storage for persistent files
- **Full Console** — Management UI with billing/credit pages
- **Turnstile placeholders** — Bot protection env vars pre-configured
- **Fake-IP trust disabled** — `ARKLOOP_OUTBOUND_TRUST_FAKE_IP=false`

Bootstrap admin URL generation is skipped — SaaS deployments handle admin provisioning through the CI/CD pipeline or OAuth/SSO integration.

---

## 1. Gateway Security Hardening

When exposing Arkloop to the public internet, configure the Gateway for production security.

### IP Mode

Set the Gateway IP detection mode to match your infrastructure:

```bash
# Direct internet access (no proxy)
ARKLOOP_GATEWAY_IP_MODE=direct

# Behind Cloudflare proxy
ARKLOOP_GATEWAY_IP_MODE=cloudflare

# Behind a trusted reverse proxy (Nginx, Caddy, etc.)
ARKLOOP_GATEWAY_IP_MODE=trusted_proxy
ARKLOOP_GATEWAY_TRUSTED_CIDRS=10.0.0.0/8,172.16.0.0/12
```

### Rate Limiting

The Gateway uses a token-bucket algorithm with per-user rate limiting via Redis:

```bash
# Burst capacity (max tokens)
ARKLOOP_RATELIMIT_CAPACITY=600

# Sustained rate (tokens refilled per minute)
ARKLOOP_RATELIMIT_RATE_PER_MINUTE=300
```

For SaaS deployments with high traffic, consider adjusting these values. The Gateway uses Redis DB 1 (`ARKLOOP_GATEWAY_REDIS_URL`) for rate limit state.

### Cloudflare Turnstile (Bot Protection)

Configure Turnstile to protect public-facing authentication endpoints:

```bash
ARKLOOP_TURNSTILE_SECRET_KEY=0x4AAAAAAA...
ARKLOOP_TURNSTILE_SITE_KEY=0x4AAAAAAA...
ARKLOOP_TURNSTILE_ALLOWED_HOST=app.example.com
```

These can also be managed via the Console UI under platform settings.

### CORS Configuration

Restrict allowed origins to your actual domain(s):

```bash
ARKLOOP_GATEWAY_CORS_ALLOWED_ORIGINS=https://app.example.com,https://console.example.com
```

> **Important**: Wildcard (`*`) origins are rejected by the Gateway.

### GeoIP Filtering

Optional MaxMind GeoIP integration for geographic access control:

```bash
ARKLOOP_GEOIP_LICENSE_KEY=your_maxmind_license_key
ARKLOOP_GEOIP_DB_PATH=/data/geoip/GeoLite2-City.mmdb
```

### Risk Scoring

Set a threshold to automatically reject requests with high risk scores (0–100):

```bash
ARKLOOP_GATEWAY_RISK_REJECT_THRESHOLD=80
```

### Security Hardening Checklist

- [ ] Set `ARKLOOP_GATEWAY_IP_MODE` to match your infrastructure
- [ ] Configure rate limiting appropriate for your traffic
- [ ] Set up Cloudflare Turnstile for bot protection
- [ ] Restrict CORS origins to your domain(s)
- [ ] Set `ARKLOOP_OUTBOUND_TRUST_FAKE_IP=false` (done by `--mode saas`)
- [ ] Set `ARKLOOP_GATEWAY_TRUST_INCOMING_TRACE_ID=0` (default)
- [ ] Configure GeoIP filtering if needed
- [ ] Set a risk reject threshold

---

## 2. TLS Termination

Arkloop does not handle TLS directly. Use a reverse proxy or tunnel service in front of the Gateway.

### Option A: Nginx

```nginx
server {
    listen 443 ssl http2;
    server_name app.example.com;

    ssl_certificate     /etc/letsencrypt/live/app.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/app.example.com/privkey.pem;

    # Security headers
    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains" always;
    add_header X-Content-Type-Options nosniff always;
    add_header X-Frame-Options DENY always;

    # SSE support
    proxy_buffering off;
    proxy_cache off;

    location / {
        proxy_pass http://127.0.0.1:19000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        # SSE: disable buffering for streaming responses
        proxy_read_timeout 86400s;
    }
}

server {
    listen 80;
    server_name app.example.com;
    return 301 https://$host$request_uri;
}
```

When using Nginx, set:
```bash
ARKLOOP_GATEWAY_IP_MODE=trusted_proxy
ARKLOOP_GATEWAY_TRUSTED_CIDRS=127.0.0.1/32
```

### Option B: Caddy

```
app.example.com {
    reverse_proxy 127.0.0.1:19000 {
        flush_interval -1
    }
}
```

Caddy automatically provisions and renews TLS certificates via Let's Encrypt. Set:
```bash
ARKLOOP_GATEWAY_IP_MODE=trusted_proxy
ARKLOOP_GATEWAY_TRUSTED_CIDRS=127.0.0.1/32
```

### Option C: Cloudflare Tunnel

```bash
cloudflared tunnel --url http://127.0.0.1:19000
```

Or with a configuration file:
```yaml
tunnel: <TUNNEL_ID>
credentials-file: /root/.cloudflared/<TUNNEL_ID>.json

ingress:
  - hostname: app.example.com
    service: http://127.0.0.1:19000
  - service: http_status:404
```

When using Cloudflare Tunnel, set:
```bash
ARKLOOP_GATEWAY_IP_MODE=cloudflare
```

### CORS Update

After configuring TLS, update the CORS origins to use `https://`:
```bash
ARKLOOP_GATEWAY_CORS_ALLOWED_ORIGINS=https://app.example.com
```

---

## 3. CI/CD Pipeline Integration

Use `setup.sh` with `--non-interactive` and `--mode saas` for automated deployments.

### GitHub Actions Example

```yaml
name: Deploy Arkloop SaaS

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Deploy
        env:
          ARKLOOP_POSTGRES_PASSWORD: ${{ secrets.POSTGRES_PASSWORD }}
          ARKLOOP_REDIS_PASSWORD: ${{ secrets.REDIS_PASSWORD }}
          ARKLOOP_AUTH_JWT_SECRET: ${{ secrets.JWT_SECRET }}
          ARKLOOP_ENCRYPTION_KEY: ${{ secrets.ENCRYPTION_KEY }}
          ARKLOOP_S3_SECRET_KEY: ${{ secrets.S3_SECRET_KEY }}
          ARKLOOP_TURNSTILE_SECRET_KEY: ${{ secrets.TURNSTILE_SECRET_KEY }}
          ARKLOOP_TURNSTILE_SITE_KEY: ${{ secrets.TURNSTILE_SITE_KEY }}
        run: |
          ./setup.sh install \
            --mode saas \
            --profile standard \
            --non-interactive \
            --prod
```

### Environment Variables

For CI/CD, pre-set secrets as environment variables **before** running `setup.sh`. The installer will use existing values instead of generating new ones:

| Secret | Generator | Description |
|--------|-----------|-------------|
| `ARKLOOP_POSTGRES_PASSWORD` | `openssl rand -hex 16` | Database password |
| `ARKLOOP_REDIS_PASSWORD` | `openssl rand -hex 16` | Redis auth |
| `ARKLOOP_AUTH_JWT_SECRET` | `openssl rand -base64 48` | JWT signing key (≥32 chars) |
| `ARKLOOP_ENCRYPTION_KEY` | `openssl rand -hex 32` | AES-256-GCM key (64 hex chars) |
| `ARKLOOP_S3_SECRET_KEY` | `openssl rand -hex 32` | Object storage auth |
| `ARKLOOP_SANDBOX_AUTH_TOKEN` | `openssl rand -hex 32` | Worker↔Sandbox auth |
| `ARKLOOP_TURNSTILE_SECRET_KEY` | Cloudflare dashboard | Bot protection |

### Upgrade Pipeline

```bash
# Pull latest images and restart
./setup.sh install --mode saas --profile standard --non-interactive --prod
```

The `install` command is idempotent — it updates `.env` and restarts services with the latest configuration. For zero-downtime upgrades, use rolling restarts:

```bash
docker compose -f compose.yaml -f compose.prod.yaml --profile pgbouncer --profile s3 --profile console-full \
  pull && \
docker compose -f compose.yaml -f compose.prod.yaml --profile pgbouncer --profile s3 --profile console-full \
  up -d --remove-orphans
```

---

## 4. Multi-Instance Scaling

For high-traffic SaaS deployments, consider horizontal scaling:

### API / Worker

Run multiple instances of `api` and `worker` behind a load balancer. PGBouncer handles connection pooling across instances:

```bash
docker compose -f compose.yaml -f compose.prod.yaml --profile pgbouncer --profile s3 \
  up -d --scale api=3 --scale worker=3
```

### PGBouncer Tuning

Adjust pool sizes for your workload:

```bash
# Total connections to PostgreSQL (shared across all clients)
ARKLOOP_PGBOUNCER_POOL_SIZE=200

# Maximum client connections to PGBouncer
ARKLOOP_PGBOUNCER_MAX_CLIENT_CONN=1000
```

### Application Connection Pools

When using PGBouncer, tune the per-instance application pool:

```bash
# Per-instance pgxpool settings (lower than direct connection)
ARKLOOP_API_DB_POOL_MAX_CONNS=5
ARKLOOP_API_DB_POOL_MIN_CONNS=0
```

### External Database

For production SaaS, consider using a managed PostgreSQL service (e.g., AWS RDS, Supabase). Update the connection strings:

```bash
DATABASE_URL=postgresql://user:pass@rds-host:5432/arkloop
ARKLOOP_DATABASE_URL=postgresql://user:pass@rds-host:5432/arkloop
```

When using an external database, PGBouncer can be disabled as managed services typically include their own connection pooling.

### External Object Storage

Replace SeaweedFS with a managed S3 service:

```bash
ARKLOOP_STORAGE_BACKEND=s3
ARKLOOP_S3_ACCESS_KEY=your_access_key
ARKLOOP_S3_SECRET_KEY=your_secret_key
ARKLOOP_S3_ENDPOINT=https://s3.amazonaws.com
ARKLOOP_S3_REGION=us-east-1
ARKLOOP_S3_BUCKET=arkloop
```
