#!/usr/bin/env bash
# Cancel stale bootstrap pool campaigns and reset bot state for a fresh rc15 cycle.
#
# Hub coordinator (remote):
#   NODE_SSH=hackme-vps bash scripts/ops/bootstrap_customer/bootstrap_fresh_rc15_cycle.sh
#
# Bootstrap VPS (local on box):
#   bash /opt/hackme-bootstrap/scripts/bootstrap_customer/bootstrap_fresh_rc15_cycle.sh --reset-bot
#
# Full cycle from dev machine (needs BOOTSTRAP_VPS_PASS):
#   NODE_SSH=hackme-vps BOOTSTRAP_VPS_PASS='...' bash scripts/ops/bootstrap_customer/bootstrap_fresh_rc15_cycle.sh --all
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
COORD_DB="${COORD_SQL_DB:-/opt/hackme/data/coordinator_fuzz.db}"
BOOT_HOST="${BOOTSTRAP_VPS:-root@89.150.41.40}"
BOOT_PASS="${BOOTSTRAP_VPS_PASS:-}"

MODE="${1:-coord}"
[[ "$MODE" == "--all" || "$MODE" == "--reset-bot" ]] || MODE="coord"

read_secret() {
  local f="$1"
  [[ -f "$f" && -r "$f" ]] || return 1
  tr -d '\r\n' <"$f"
}

COORD_ADMIN="${COORD_ADMIN_TOKEN:-${COORD_ADMIN:-}}"
if [[ -z "$COORD_ADMIN" ]]; then
  for f in \
    "$ROOT/.secrets/hackme_coordinator_admin_token" \
    "$INSTALL/.secrets/coordinator_admin.token" \
    /etc/hackme/coordinator-cleanup.env; do
    if [[ "$f" == *.env && -r "$f" ]]; then
      # shellcheck disable=SC1090
      set -a && . "$f" && set +a
      COORD_ADMIN="${COORD_ADMIN_TOKEN:-${COORD_ADMIN:-}}"
      [[ -n "$COORD_ADMIN" ]] && break
      continue
    fi
    if tok="$(read_secret "$f" 2>/dev/null)"; then
      COORD_ADMIN="$tok"
      break
    fi
  done
fi

log() { echo "[bootstrap-fresh-rc15] $*"; }

run_sql() {
  local sql="$1"
  if [[ -n "${NODE_SSH:-}" ]]; then
    ssh -o BatchMode=yes "$NODE_SSH" "sqlite3 -cmd '.timeout 60000' \"${COORD_DB}\" \"${sql}\""
  elif [[ -f "$COORD_DB" ]]; then
    sqlite3 -cmd '.timeout 60000' "$COORD_DB" "$sql"
  else
    return 1
  fi
}

cancel_coord_campaign() {
  local id="$1"
  if [[ -z "$COORD_ADMIN" ]]; then
    log "skip coord cancel $id (no admin token)"
    return 0
  fi
  if [[ -n "${NODE_SSH:-}" ]]; then
    if ssh -o BatchMode=yes "$NODE_SSH" \
      "curl -fsS -X POST http://127.0.0.1:18081/api/fuzz/pool/campaigns/status \
        -H 'X-Hackme-Admin-Token: ${COORD_ADMIN}' -H 'Content-Type: application/json' \
        -d '{\"id\":\"${id}\",\"status\":\"cancelled\"}'" >/dev/null 2>&1; then
      log "coord cancelled $id"
      return 0
    fi
  elif curl -fsS -X POST "${COORD_URL%/}/api/fuzz/pool/campaigns/status" \
    -H "X-Hackme-Admin-Token: ${COORD_ADMIN}" \
    -H "Content-Type: application/json" \
    -d "{\"id\":\"${id}\",\"status\":\"cancelled\"}" >/dev/null 2>&1; then
    log "coord cancelled $id"
    return 0
  fi
  run_sql "UPDATE fuzz_campaigns SET status='cancelled', completed_at=strftime('%s','now') WHERE id='${id}' AND status IN ('running','planned'); UPDATE fuzz_work_items SET status='cancelled', updated_at=strftime('%s','now') WHERE campaign_id='${id}' AND status IN ('pending','leased');" \
    && log "sql cancelled $id" || log "WARN failed cancel $id"
}

cancel_hub_campaign() {
  local id="$1"
  local admin="${ADMIN_TOKEN:-}"
  [[ -n "$admin" ]] || admin="$(read_secret "$ROOT/.secrets/hackme_admin_token" 2>/dev/null || true)"
  if [[ -z "$admin" && -n "${NODE_SSH:-}" ]]; then
    admin="$(ssh -o BatchMode=yes "$NODE_SSH" "grep -m1 '^HACKME_ADMIN_TOKEN=' /opt/hackme/.env.vps | cut -d= -f2- | tr -d '\r\n'" 2>/dev/null || true)"
  fi
  [[ -n "$admin" ]] || { log "skip hub cancel $id (no admin)"; return 0; }
  local base="${HUB_BASE:-https://hackme.tech}"
  curl -fsS -X POST "${base%/}/api/fuzz/campaigns/${id}/status" \
    -H "X-Hackme-Admin-Token: ${admin}" \
    -H "Content-Type: application/json" \
    -d '{"status":"cancelled"}' >/dev/null 2>&1 \
    && log "hub cancelled $id" || log "WARN hub cancel $id failed (may already be done)"
}

cancel_bootstrap_campaigns() {
  log "listing bootstrap campaigns on coordinator"
  local rows
  rows="$(run_sql "SELECT id||'|'||status FROM fuzz_campaigns WHERE id LIKE 'campaign-bootstrap-%' AND status IN ('running','planned') ORDER BY created_at DESC;" 2>/dev/null || true)"
  if [[ -z "$rows" ]]; then
    log "no running/planned bootstrap campaigns"
    return 0
  fi
  while IFS='|' read -r cid status; do
    [[ -z "$cid" ]] && continue
    log "cancel $cid ($status)"
    cancel_coord_campaign "$cid"
    cancel_hub_campaign "$cid"
  done <<<"$rows"
  NODE_SSH="${NODE_SSH:-}" bash "$ROOT/scripts/ops/coordinator_fuzz_queue_cleanup.sh" || true
}

reset_bootstrap_bot() {
  log "reset bot state on $INSTALL"
  mkdir -p "$INSTALL/logs/bootstrap"
  python3 - <<PY
import json, datetime as d, pathlib
p = pathlib.Path("$INSTALL/logs/bootstrap/bot_state.json")
st = {
  "target_idx": 0,
  "plan_until_utc": (d.datetime.now(d.timezone.utc) + d.timedelta(days=3)).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "cadence": "2_3_per_day_light_3d",
  "rc15_cycle": True,
  "reset_utc": d.datetime.now(d.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
}
p.write_text(json.dumps(st, indent=2) + "\n")
print("wrote", p)
PY
}

remote_reset_bootstrap() {
  [[ -n "$BOOT_PASS" ]] || { log "set BOOTSTRAP_VPS_PASS for remote reset"; return 1; }
  python3 <<PY
import os, paramiko, pathlib, sys
host = "$BOOT_HOST".split("@")[-1]
user = "$BOOT_HOST".split("@")[0] if "@" in "$BOOT_HOST" else "root"
pw = os.environ.get("BOOTSTRAP_VPS_PASS", "$BOOT_PASS")
install = "$INSTALL"
root = pathlib.Path("$ROOT")

c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(host, username=user, password=pw, timeout=30)
sftp = c.open_sftp()

def run(cmd, timeout=600):
    print(">>>", cmd[:140])
    stdin, stdout, stderr = c.exec_command(cmd, timeout=timeout)
    out = stdout.read().decode()
    err = stderr.read().decode()
    rc = stdout.channel.recv_exit_status()
    if out: print(out[-3000:])
    if err: print(err[-1500:], file=sys.stderr)
    if rc != 0:
        raise SystemExit(f"rc={rc}")
    return out

run(f"mkdir -p {install}/scripts/bootstrap_customer {install}/logs/bootstrap")
for name in ["bootstrap_bot.sh", "place_bootstrap_order.sh", "bootstrap_resync_pool.sh", "bootstrap_snapshot.sh", "bootstrap_fresh_rc15_cycle.sh"]:
    local = root / "scripts/ops/bootstrap_customer" / name
    if local.exists():
        sftp.put(str(local), f"{install}/scripts/bootstrap_customer/{name}")
        run(f"chmod +x {install}/scripts/bootstrap_customer/{name}")
run(f"BOOTSTRAP_INSTALL={install} bash {install}/scripts/bootstrap_customer/bootstrap_fresh_rc15_cycle.sh --reset-bot")
run(f"systemctl daemon-reload; systemctl restart hackme-bootstrap-workerfuzz.service 2>/dev/null || true")
c.close()
print("bootstrap VPS reset ok")
PY
}

place_test_orders() {
  [[ -n "$BOOT_PASS" ]] || { log "skip test orders (no BOOTSTRAP_VPS_PASS)"; return 0; }
  python3 <<PY
import os, paramiko, time
host = "$BOOT_HOST".split("@")[-1]
user = "$BOOT_HOST".split("@")[0] if "@" in "$BOOT_HOST" else "root"
pw = os.environ.get("BOOTSTRAP_VPS_PASS", "$BOOT_PASS")
install = "$INSTALL"
targets = ["jsmn", "parser_expat"]

c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(host, username=user, password=pw, timeout=30)

def run(cmd, timeout=900):
    print(">>>", cmd[:160])
    stdin, stdout, stderr = c.exec_command(cmd, timeout=timeout)
    out = stdout.read().decode()
    err = stderr.read().decode()
    rc = stdout.channel.recv_exit_status()
    print(out[-4000:])
    if err: print(err[-2000:])
    return rc, out

for t in targets:
    cmd = (
        f"export BOOTSTRAP_INSTALL={install} HACKME_MINIMAL_POH_GATE=1 "
        f"BUDGET_HMC=5 BUDGET_RUNS=256 TARGET_SOLVES=4 POLL_SEC=30 MAX_WAIT=420; "
        f"bash {install}/scripts/bootstrap_customer/place_bootstrap_order.sh {t}"
    )
    rc, out = run(cmd, timeout=900)
    print(f"order {t} rc={rc}")
    time.sleep(5)
c.close()
PY
}

case "$MODE" in
  --reset-bot)
    reset_bootstrap_bot
    ;;
  --all)
    cancel_bootstrap_campaigns
    remote_reset_bootstrap
    place_test_orders
    ;;
  *)
    cancel_bootstrap_campaigns
    ;;
esac

log "done mode=$MODE"
