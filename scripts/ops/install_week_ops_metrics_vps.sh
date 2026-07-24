#!/usr/bin/env bash
# Install 7-day week ops metrics timer on the hub VPS.
# Samples every 15 minutes → /opt/hackme/reports/week-ops-metrics/
set -euo pipefail
ROOT="${HACKME_ROOT:-/opt/hackme}"
SCRIPT_SRC="${1:-}"
if [[ -z "$SCRIPT_SRC" ]]; then
  SCRIPT_SRC="$(cd "$(dirname "$0")" && pwd)/week_ops_metrics_once.sh"
fi
BRIEFLY_SRC="$(dirname "$SCRIPT_SRC")/week_ops_briefing.sh"

install -d -m 755 "$ROOT/scripts/ops" "$ROOT/reports/week-ops-metrics" "$ROOT/logs"
install -m 755 "$SCRIPT_SRC" "$ROOT/scripts/ops/week_ops_metrics_once.sh"
if [[ -f "$BRIEFLY_SRC" ]]; then
  install -m 755 "$BRIEFLY_SRC" "$ROOT/scripts/ops/week_ops_briefing.sh"
fi

cat >/etc/systemd/system/hackme-week-ops-metrics.service <<EOF
[Unit]
Description=HackMe week ops metrics sample (pool/fuzz/miners/host/exchange)
After=network-online.target hackme-node.service
Wants=network-online.target

[Service]
Type=oneshot
Environment=HACKME_ROOT=$ROOT
Environment=WEEK_OPS_DIR=$ROOT/reports/week-ops-metrics
ExecStart=$ROOT/scripts/ops/week_ops_metrics_once.sh
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
EOF

# Every 15 minutes; OnCalendar keeps firing until timer disabled/stopped after the week.
cat >/etc/systemd/system/hackme-week-ops-metrics.timer <<'EOF'
[Unit]
Description=HackMe week ops metrics every 15m (keep ~7 days then stop)

[Timer]
OnBootSec=2min
OnUnitActiveSec=15min
AccuracySec=1min
Persistent=true
Unit=hackme-week-ops-metrics.service

[Install]
WantedBy=timers.target
EOF

# Auto-stop helper after 7d (oneshot timer)
cat >/etc/systemd/system/hackme-week-ops-metrics-stop.service <<EOF
[Unit]
Description=Stop HackMe week ops metrics timer after planned window
After=hackme-week-ops-metrics.timer

[Service]
Type=oneshot
ExecStart=/bin/systemctl disable --now hackme-week-ops-metrics.timer
ExecStart=/bin/bash -c 'echo stopped_at=\$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ) >> $ROOT/reports/week-ops-metrics/STOPPED.txt'
EOF

cat >/etc/systemd/system/hackme-week-ops-metrics-stop.timer <<'EOF'
[Unit]
Description=Stop week ops metrics after 7 days from enable

[Timer]
OnActiveSec=7d
AccuracySec=1h
Unit=hackme-week-ops-metrics-stop.service

[Install]
WantedBy=timers.target
EOF

systemctl daemon-reload
systemctl enable --now hackme-week-ops-metrics.timer
systemctl enable --now hackme-week-ops-metrics-stop.timer
# First sample immediately
systemctl start hackme-week-ops-metrics.service || true

echo "installed: hackme-week-ops-metrics.timer (15m) + stop after 7d"
systemctl list-timers 'hackme-week-ops-metrics*' --no-pager || true
ls -la "$ROOT/reports/week-ops-metrics/" || true
echo "briefing: bash $ROOT/scripts/ops/week_ops_briefing.sh"
