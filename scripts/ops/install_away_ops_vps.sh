#!/usr/bin/env bash
# Install 48h+ away ops on hub: libheif 24h systemd, fuzz watchdog, pool snapshot timer.
# Run ON VPS as root: bash /opt/hackme/scripts/ops/install_away_ops_vps.sh
set -euo pipefail
ROOT="${HACKME_ROOT:-/opt/hackme}"
ANCHOR_EPOCH="${ANCHOR_EPOCH:-1784543563}"
UNIT_DIR="/etc/systemd/system"

[[ -d "$ROOT/scripts/ops" ]] || { echo "missing $ROOT"; exit 1; }
chmod +x "$ROOT/scripts/ops/oss_cve_libheif_watchdog_once.sh" \
  "$ROOT/scripts/ops/pool_away_watch_once.sh" \
  "$ROOT/scripts/ops/run_oss_cve_watch_libheif_24h_cadence.sh" 2>/dev/null || true

cat >"$UNIT_DIR/hackme-libheif-24h.service" <<EOF
[Unit]
Description=HackMe OSS CVE Watch libheif 24h cadence (Day 1-14)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$ROOT
Environment=HACKME_ROOT=$ROOT
Environment=TARGET=libheif
Environment=START_DAY=1
Environment=END_DAY=14
Environment=DAY_SEC=86400
Environment=ANCHOR_EPOCH=$ANCHOR_EPOCH
Environment=SKIP_REBUILD=1
Environment=GIT_PUSH=1
Environment=NODE_SSH=
ExecStart=/usr/bin/bash $ROOT/scripts/ops/run_oss_cve_watch_libheif_24h_cadence.sh
Restart=on-failure
RestartSec=30
StandardOutput=append:$ROOT/logs/hackme-libheif-24h.service.log
StandardError=append:$ROOT/logs/hackme-libheif-24h.service.log

[Install]
WantedBy=multi-user.target
EOF

cat >"$UNIT_DIR/hackme-libheif-fuzzer-watchdog.service" <<EOF
[Unit]
Description=HackMe libheif fuzzer watchdog (one-shot)
After=network-online.target

[Service]
Type=oneshot
Environment=HACKME_ROOT=$ROOT
ExecStart=/usr/bin/bash $ROOT/scripts/ops/oss_cve_libheif_watchdog_once.sh
EOF

cat >"$UNIT_DIR/hackme-libheif-fuzzer-watchdog.timer" <<'EOF'
[Unit]
Description=HackMe libheif fuzzer watchdog every 10 min

[Timer]
OnBootSec=5min
OnUnitActiveSec=10min
Persistent=true

[Install]
WantedBy=timers.target
EOF

cat >"$UNIT_DIR/hackme-pool-away-watch.service" <<EOF
[Unit]
Description=HackMe pool + libheif away snapshot (one-shot)
After=network-online.target

[Service]
Type=oneshot
Environment=HACKME_ROOT=$ROOT
ExecStart=/usr/bin/bash $ROOT/scripts/ops/pool_away_watch_once.sh
EOF

cat >"$UNIT_DIR/hackme-pool-away-watch.timer" <<'EOF'
[Unit]
Description=HackMe pool away watch every 30 min

[Timer]
OnBootSec=3min
OnUnitActiveSec=30min
Persistent=true

[Install]
WantedBy=timers.target
EOF

systemctl daemon-reload
systemctl enable hackme-libheif-24h.service \
  hackme-libheif-fuzzer-watchdog.timer \
  hackme-pool-away-watch.timer

# Hand off from ad-hoc bash cadence to systemd (keep live file_fuzzer).
if pgrep -f 'run_oss_cve_watch_libheif_24h_cadence' >/dev/null 2>&1; then
  echo "[away-ops] stopping legacy bash cadence wrappers (fuzzer stays up)"
  pkill -f 'run_oss_cve_watch_libheif_24h_cadence' 2>/dev/null || true
  sleep 3
fi
systemctl restart hackme-libheif-24h.service

systemctl start hackme-libheif-fuzzer-watchdog.timer
systemctl start hackme-pool-away-watch.timer
bash "$ROOT/scripts/ops/pool_away_watch_once.sh" || true

echo "[away-ops] installed"
systemctl is-active hackme-libheif-24h.service || true
systemctl is-active hackme-node hackme-coordinator hackme-workerfuzz
systemctl list-timers hackme-libheif-fuzzer-watchdog.timer hackme-pool-away-watch.timer --no-pager | tail -5
