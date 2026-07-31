#!/usr/bin/env bash
# Run ON bootstrap VPS (root) — customer node + bot. Idempotent.
set -euo pipefail
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
VER="${VER:-0.1.0-rc11s}"
AUTH_BASE="${AUTH_BASE:-https://hackme.tech}"
MINER_SEED_FILE="${MINER_SEED_FILE:-/opt/hackme-miner/.secrets/miner.seed.hex}"

log() { echo "[bootstrap-setup $(date -u +%H:%M:%S)] $*"; }

apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl jq xxd python3 openssl ca-certificates >/dev/null

mkdir -p "$INSTALL"/{data,logs/bootstrap,.secrets,tasks/artifacts/security,scripts/bootstrap_customer}
chmod 700 "$INSTALL/.secrets" "$INSTALL/data"

if [[ ! -x "$INSTALL/hackme" ]]; then
  log "download hackme ${VER} linux tarball"
  tmp="/tmp/hackme_${VER}_linux.tar.gz"
  curl -fL --retry 3 --connect-timeout 20 --max-time 600 \
    -o "$tmp" "https://hackme.tech/dist/release_${VER}/hackme_${VER}_linux.tar.gz" \
    || curl -fL -k -H "Host: hackme.tech" -o "$tmp" \
    "https://132.243.112.100/dist/release_${VER}/hackme_${VER}_linux.tar.gz"
  tar -xzf "$tmp" -C /tmp
  cp -a /tmp/linux/* "$INSTALL/"
  chmod +x "$INSTALL/hackme" "$INSTALL/bin/"* 2>/dev/null || true
fi

# Unified wallet = miner payout address (mining balance usable for orders)
if [[ -f "$MINER_SEED_FILE" ]]; then
  seed="$(tr -d '\r\n' <"$MINER_SEED_FILE")"
  mkdir -p "$INSTALL/data"
  printf '%s' "$seed" >"$INSTALL/data/node_ed25519.seed"
  chmod 600 "$INSTALL/data/node_ed25519.seed"
  log "linked wallet to miner seed (unified payout address)"
fi

if [[ ! -f "$INSTALL/pool.miner.token" ]]; then
  curl -fsSL "$AUTH_BASE/pool.miner.token" -o "$INSTALL/pool.miner.token" 2>/dev/null \
    || echo "REPLACE_WITH_POOL_TOKEN" >"$INSTALL/pool.miner.token"
fi

if [[ ! -f "$INSTALL/.env" ]]; then
  POOL_TOKEN="$(tr -d '\r\n' <"$INSTALL/pool.miner.token")"
  ADMIN="$(openssl rand -hex 24)"
  cat >"$INSTALL/.env" <<EOF
HACKME_BIND_ADDR=127.0.0.1:8080
HACKME_ADMIN_TOKEN=${ADMIN}
HACKME_REQUIRE_ADMIN_TOKEN=1
HACKME_DESKTOP_MODE=0
HACKME_UNIFIED_MINER_NODE_SEED=1
HACKME_PUBLIC_AUTHORITY_BASE=${AUTH_BASE}
HACKME_CANONICAL_CHAIN_URL=${AUTH_BASE}
HACKME_POOL_COORDINATOR_URL=${AUTH_BASE}/pool/coordinator
HACKME_POOL_COORDINATOR_TOKEN=${POOL_TOKEN}
HACKME_WORKER_WATCHDOG=0
HACKME_GPU_DISABLE=1
HACKME_DATA_DIR=${INSTALL}/data
EOF
  chmod 600 "$INSTALL/.env"
fi

# systemd service
cat >/etc/systemd/system/hackme-bootstrap-node.service <<EOF
[Unit]
Description=HackMe bootstrap customer node
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${INSTALL}
EnvironmentFile=${INSTALL}/.env
ExecStart=${INSTALL}/hackme
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable hackme-bootstrap-node.service
systemctl restart hackme-bootstrap-node.service

log "wait node /api/status"
for i in $(seq 1 90); do
  if curl -fsS --max-time 5 http://127.0.0.1:8080/api/status?lite=1 >/dev/null 2>&1; then
    log "node up"
    break
  fi
  sleep 2
done

curl -fsS --max-time 15 http://127.0.0.1:8080/api/wallet \
  -H "X-Hackme-Admin-Token: $(grep '^HACKME_ADMIN_TOKEN=' $INSTALL/.env | cut -d= -f2-)" | jq -c '{address,balance_hmc,balance_on_chain_hmc}' || true

# Mirror canonical on-chain balance into local wallet row for fuzz escrow spends (follower/desktop).
python3 <<'PY' "$INSTALL"
import json, pathlib, subprocess, sys, urllib.request
install = pathlib.Path(sys.argv[1])
admin = ""
for line in (install / ".env").read_text().splitlines():
    if line.startswith("HACKME_ADMIN_TOKEN="):
        admin = line.split("=", 1)[1].strip()
        break
if not admin:
    raise SystemExit(0)
req = urllib.request.Request(
    f"http://127.0.0.1:8080/api/wallet",
    headers={"X-Hackme-Admin-Token": admin},
)
with urllib.request.urlopen(req, timeout=15) as r:
    w = json.load(r)
units = int(w.get("balance_on_chain_units") or w.get("balance_units") or 0)
addr = w.get("address") or ""
if units <= 0 or not addr:
    raise SystemExit(0)
db = install / "data" / "hackme.db"
subprocess.run([
    "sqlite3", str(db),
    f"UPDATE wallet SET balance_units={units}, balance_hmc={units}/100000000.0 WHERE id=1;"
    f"UPDATE accounts SET balance_units={units} WHERE address='{addr}';",
], check=False)
PY

log "setup done install=$INSTALL"
