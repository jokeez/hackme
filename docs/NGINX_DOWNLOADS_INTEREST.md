# Nginx interest tracker — downloads.html & HackMe OS ISO

Track how many visitors open **downloads.html** and start the **956 MB** `HackMe-OS-0.1.0-rc11g-amd64.iso` on the canonical VPS.

## On VPS `132.243.112.100`

Scripts live under `/opt/hackme/scripts/ops/` (deploy from repo or rsync).

### 1) Real client IPs (recommended, once)

Default `/var/log/nginx/access.log` only shows **Cloudflare edge** IPs. Enable a sidecar log with `CF-Connecting-IP`:

```bash
sudo bash /opt/hackme/scripts/ops/vps_enable_nginx_client_ip_log.sh
```

New log: `/var/log/nginx/hackme-site-clients.log`

### 2) Real-time dashboard

```bash
cd /opt/hackme
bash scripts/ops/nginx_downloads_interest.sh live
```

Refreshes every 2s; rolling window default **60 minutes**. Change window:

```bash
bash scripts/ops/nginx_downloads_interest.sh live --window-minutes 180
```

### 3) Reports

```bash
# Since ISO publish day
bash scripts/ops/nginx_downloads_interest.sh report --since 2026-05-22

# Last hour / last 24h
bash scripts/ops/nginx_downloads_interest.sh report --minutes 60
bash scripts/ops/nginx_downloads_interest.sh report --minutes 1440

# Include bots (curl, healthchecks)
bash scripts/ops/nginx_downloads_interest.sh report --since 2026-05-22 --include-bots
```

### Metrics explained

| Metric | Meaning |
|--------|---------|
| **unique visitors (fp)** | SHA fingerprint of client IP + User-Agent (best effort behind CF) |
| **unique client IPs** | Real IPs when `hackme-site-clients.log` is enabled |
| **iso clickers** | GET/HEAD to `.iso` with referer `downloads.html` |
| **unique downloaders** | At least one **200/206** response on the ISO path (partial range counts) |

Bots (curl, `Go-http-client`, `HackMe-Verdict`, etc.) are excluded by default.

## From your laptop (SSH)

```bash
ssh -i ~/.ssh/your_key root@132.243.112.100 \
  'cd /opt/hackme && bash scripts/ops/nginx_downloads_interest.sh report --minutes 60'
```

## Env overrides

| Variable | Default |
|----------|---------|
| `NGINX_ACCESS_LOG` | `/var/log/nginx/access.log` |
| `NGINX_CLIENT_LOG` | `/var/log/nginx/hackme-site-clients.log` (auto if exists) |
| `HACKME_ISO_SUBPATH` | `HackMe-OS-0.1.0-rc11g-amd64.iso` |
