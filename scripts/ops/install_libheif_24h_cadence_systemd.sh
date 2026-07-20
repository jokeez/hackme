#!/usr/bin/env bash
# Install systemd --user unit for libheif 24/7 24h-day cadence on VPS hub.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
UNIT="${HOME}/.config/systemd/user/hackme-libheif-24h.service"
mkdir -p "${HOME}/.config/systemd/user"
cat >"$UNIT" <<EOF
[Unit]
Description=HackMe OSS CVE Watch libheif 24h cadence
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$ROOT
Environment=TARGET=libheif
Environment=START_DAY=1
Environment=END_DAY=14
Environment=DAY_SEC=86400
Environment=SKIP_REBUILD=1
Environment=NODE_SSH=hackme-vps
Environment=GIT_PUSH=1
ExecStart=/usr/bin/bash $ROOT/scripts/ops/run_oss_cve_watch_libheif_24h_cadence.sh
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable hackme-libheif-24h.service
echo "[install] unit=$UNIT"
echo "Start: systemctl --user start hackme-libheif-24h.service"
echo "Logs:  journalctl --user -u hackme-libheif-24h.service -f"
