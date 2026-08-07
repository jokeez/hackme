#!/usr/bin/env bash
# Install /etc/hackme/coordinator-cleanup.env for hackme-pool-fuzz-queue-cleanup.timer.
# Run on hub as root:
#   sudo bash scripts/ops/install_coordinator_fuzz_cleanup_env.sh
set -euo pipefail

ENV_FILE="/etc/hackme/coordinator-cleanup.env"
TOKEN="${COORD_ADMIN_TOKEN:-}"

if [[ -z "$TOKEN" ]]; then
  for f in \
    /opt/hackme/.secrets/hackme_coordinator_admin_token \
    /opt/hackme/.secrets/coordinator_admin.token; do
    if [[ -f "$f" ]]; then
      TOKEN="$(tr -d '\r\n' <"$f")"
      break
    fi
  done
fi

[[ -n "$TOKEN" ]] || {
  echo "[install-cleanup-env] set COORD_ADMIN_TOKEN or place token in /opt/hackme/.secrets/" >&2
  exit 1
}

mkdir -p /etc/hackme
umask 077
cat >"$ENV_FILE" <<EOF
# HackMe coordinator cleanup (read by systemd, injected into hackme user service)
COORD_ADMIN_TOKEN=${TOKEN}
COORD_URL=http://127.0.0.1:18081
COORD_SQL_DB=/opt/hackme/data/coordinator_fuzz.db
EOF
chmod 600 "$ENV_FILE"
chown root:root "$ENV_FILE"
echo "[install-cleanup-env] wrote $ENV_FILE"
echo "[install-cleanup-env] note: coordinator binary on hub is /opt/hackme/coordinator (not bin/)"

if [[ -f "$(dirname "$0")/systemd/hackme-pool-fuzz-queue-cleanup.service" ]]; then
  install -m 0644 "$(dirname "$0")/systemd/hackme-pool-fuzz-queue-cleanup.service" \
    /etc/systemd/system/hackme-pool-fuzz-queue-cleanup.service
  install -m 0644 "$(dirname "$0")/systemd/hackme-pool-fuzz-queue-cleanup.service" \
    /opt/hackme/scripts/ops/systemd/hackme-pool-fuzz-queue-cleanup.service 2>/dev/null || true
  systemctl daemon-reload
  systemctl enable --now hackme-pool-fuzz-queue-cleanup.timer 2>/dev/null || true
  echo "[install-cleanup-env] systemd unit installed; running cleanup once…"
  systemctl start hackme-pool-fuzz-queue-cleanup.service
fi
