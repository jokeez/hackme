#!/usr/bin/env bash
# VPS: nginx log rotation tuned for high-volume hackme-site-clients.log (pool polls).
#
# Problem: default /etc/logrotate.d/nginx uses *.log + delaycompress → multi-GB .1 files.
# This script:
#   1) Rotates only access/error in stock nginx config (excludes hackme-site-clients.log)
#   2) Adds /etc/logrotate.d/hackme-nginx-clients (maxsize, compress, keep 7)
#   3) Hourly force rotate for clients log via /etc/cron.d/hackme-nginx-logrotate
#   4) Optional one-shot: compress stale huge .1 / vacuum old .gz
#
# Run on VPS as root:
#   NODE_SSH=hackme-vps bash scripts/ops/vps_setup_nginx_logrotate.sh
#   NODE_SSH=hackme-vps VACUUM_NOW=1 bash scripts/ops/vps_setup_nginx_logrotate.sh
#
set -euo pipefail

NODE_SSH="${NODE_SSH:-}"
VACUUM_NOW="${VACUUM_NOW:-0}"

if [[ -z "$NODE_SSH" ]]; then
  echo "[nginx-logrotate] set NODE_SSH=hackme-vps" >&2
  exit 1
fi

ssh -o BatchMode=yes -o ConnectTimeout=20 "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
VACUUM_NOW='${VACUUM_NOW}'

if [[ "\$(id -u)" -ne 0 ]]; then
  echo "[nginx-logrotate] run as root on VPS" >&2
  exit 1
fi

NGINX_LR=/etc/logrotate.d/nginx
HACKME_LR=/etc/logrotate.d/hackme-nginx-clients
CRON=/etc/cron.d/hackme-nginx-logrotate
CLIENT_LOG=/var/log/nginx/hackme-site-clients.log

echo "[nginx-logrotate] before:"
du -sh /var/log/nginx/hackme-site-clients.log* 2>/dev/null | head -8 || true
df -h / | tail -1

# --- stock nginx: do not rotate hackme-site-clients via wildcard ---
if [[ -f "\$NGINX_LR" ]]; then
  cp -a "\$NGINX_LR" "\${NGINX_LR}.bak.\$(date -u +%Y%m%dT%H%M%SZ)"
fi
cat >"\$NGINX_LR" <<'EOF'
/var/log/nginx/access.log
/var/log/nginx/error.log {
	daily
	missingok
	rotate 14
	compress
	delaycompress
	notifempty
	create 0640 www-data adm
	sharedscripts
	prerotate
		if [ -d /etc/logrotate.d/httpd-prerotate ]; then \
			run-parts /etc/logrotate.d/httpd-prerotate; \
		fi \
	endscript
	postrotate
		invoke-rc.d nginx rotate >/dev/null 2>&1 || true
	endscript
}
EOF
echo "[nginx-logrotate] wrote \$NGINX_LR (access/error only)"

# --- high-volume client IP log: size cap + compress immediately ---
cat >"\$HACKME_LR" <<'EOF'
/var/log/nginx/hackme-site-clients.log {
	# Pool/dashboard polls fill this fast; cap size even between daily cron runs.
	maxsize 250M
	rotate 7
	compress
	nodelaycompress
	missingok
	notifempty
	create 0640 www-data adm
	sharedscripts
	postrotate
		invoke-rc.d nginx rotate >/dev/null 2>&1 || true
	endscript
}
EOF
echo "[nginx-logrotate] wrote \$HACKME_LR (maxsize 250M, keep 7, compress)"

cat >"\$CRON" <<'EOF'
# Rotate hackme-site-clients.log when it exceeds maxsize (logrotate daily is too slow).
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin
12 * * * * root /usr/sbin/logrotate -s /var/lib/logrotate/status /etc/logrotate.d/hackme-nginx-clients >/dev/null 2>&1
EOF
chmod 644 "\$CRON"
echo "[nginx-logrotate] hourly cron: \$CRON"

# --- optional vacuum: gzip huge stale rotations, drop very old ---
if [[ "\$VACUUM_NOW" == "1" ]]; then
  echo "[nginx-logrotate] VACUUM_NOW: force rotate + compress old chunks"
  if [[ -f "\$CLIENT_LOG" ]]; then
    /usr/sbin/logrotate -f "\$HACKME_LR" || true
  fi
  for f in /var/log/nginx/hackme-site-clients.log.[0-9]*; do
    [[ -f "\$f" ]] || continue
    case "\$f" in
      *.gz) ;;
      *)
        echo "[nginx-logrotate] gzip -9 \$f"
        gzip -9 "\$f" || true
        ;;
    esac
  done
  # Drop client log archives older than 21 days (keep recent forensics).
  find /var/log/nginx -maxdepth 1 -name 'hackme-site-clients.log*.gz' -mtime +21 -delete 2>/dev/null || true
fi

echo "[nginx-logrotate] after:"
du -sh /var/log/nginx/hackme-site-clients.log* 2>/dev/null | head -10 || true
df -h / | tail -1
echo "[nginx-logrotate] OK"
REMOTE

echo "[nginx-logrotate] done on $NODE_SSH (VACUUM_NOW=${VACUUM_NOW})"
