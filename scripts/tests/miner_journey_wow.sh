#!/usr/bin/env bash
# Staged "fresh miner" journey: site → download → verify → unpack → setup → prod pool probe.
# Safe with overnight: never binds :8080 if something already listens (SKIP_LIVE_MINER_START=1).
#
#   bash scripts/tests/miner_journey_wow.sh
#   SITE_BASE=https://hackme.tech bash scripts/tests/miner_journey_wow.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

SITE="${SITE_BASE:-https://hackme.tech}"
VERSION="${VERSION:-0.1.0-rc11k}"
REL="release_${VERSION}"
DIST_URL="$SITE/dist/$REL"
COORD="${COORD_URL:-${SITE%/}/pool/coordinator}"
WORKDIR="${WORKDIR:-/tmp/hackme-miner-wow-$$}"
LOCAL_DIST="${LOCAL_DIST:-$ROOT/dist/$REL}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT_DIR:-$ROOT/reports/miner-journey-wow-$STAMP}"
REPORT_JSON="$OUT/report.json"
REPORT_MD="$OUT/REPORT.md"

mkdir -p "$OUT"
trap 'rm -rf "$WORKDIR"' EXIT
mkdir -p "$WORKDIR"

require_cmd curl
require_cmd jq
require_cmd sha256sum
require_cmd tar
require_cmd unzip
require_cmd awk

failures=0
passes=0
declare -a PHASE_LOG=()

phase() {
  PHASE_LOG+=("$1")
  echo ""
  echo "══════════════════════════════════════════════════════════"
  echo "  PHASE $1"
  echo "══════════════════════════════════════════════════════════"
}

record() {
  local id="$1" status="$2" detail="${3:-}"
  if [[ "$status" == "PASS" ]]; then
    passes=$((passes + 1))
    pass "$id${detail:+ — $detail}"
  else
    failures=$((failures + 1))
    fail_msg "$id${detail:+ — $detail}"
  fi
  printf '%s\n' "{\"id\":\"$id\",\"status\":\"$status\",\"detail\":$(jq -Rn --arg d "$detail" '$d')}" >>"$OUT/checks.jsonl"
}

fetch() {
  local name="$1" url="$2"
  local dest="$WORKDIR/$name"
  if [[ -f "$LOCAL_DIST/$name" ]]; then
    cp -f "$LOCAL_DIST/$name" "$dest"
    echo "[wow] local artifact $name" >&2
  else
    echo "[wow] download $url" >&2
    curl -fSL --max-time 600 -o "$dest" "$url"
  fi
  printf '%s\n' "$dest"
}

if curl -fsS --max-time 3 http://127.0.0.1:8080/api/status?lite=1 >/dev/null 2>&1; then
  export SKIP_LIVE_MINER_START="${SKIP_LIVE_MINER_START:-1}"
  echo "[wow] local :8080 busy — SKIP_LIVE_MINER_START=1 (overnight/desktop safe)"
fi

: >"$OUT/checks.jsonl"
phase "1 — Discover (site like a browser)"
site_paths=(
  "root:/"
  "index:/index.html"
  "downloads:/downloads.html"
  "research:/research.html"
  "fuzz_campaigns:/fuzz-campaigns.html"
  "developers:/developers.html"
  "contacts:/contacts.html"
)
for entry in "${site_paths[@]}"; do
  sid="${entry%%:*}"
  path="${entry#*:}"
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 90 "${SITE}${path}" 2>/dev/null || true)"
  code="${code:-000}"
  code="${code:0:3}"
  if [[ "$code" == "200" ]]; then
    record "site_${sid}" PASS "HTTP $code"
  else
    record "site_${sid}" FAIL "HTTP $code $path"
  fi
done

phase "2 — Download release bundle"
SHA_FILE="$(fetch SHA256SUMS.txt "$DIST_URL/SHA256SUMS.txt")"
LINUX_TGZ="$(fetch "hackme_${VERSION}_linux.tar.gz" "$DIST_URL/hackme_${VERSION}_linux.tar.gz")"
WIN_ZIP="$(fetch "hackme_${VERSION}_windows_setup.zip" "$DIST_URL/hackme_${VERSION}_windows_setup.zip")"
record "download_artifacts" PASS "SHA256SUMS + linux + windows"

phase "3 — Verify SHA256"
verify_one_sha() {
  local file="$1" sums="$2"
  local exp got
  exp="$(awk -v n="$(basename "$file")" '$NF==n {print $1; exit}' "$sums")"
  got="$(sha256sum "$file" | awk '{print $1}')"
  [[ -n "$exp" && "$exp" == "$got" ]]
}
if verify_one_sha "$LINUX_TGZ" "$SHA_FILE"; then
  record "sha256_linux" PASS
else
  record "sha256_linux" FAIL "mismatch"
fi
if verify_one_sha "$WIN_ZIP" "$SHA_FILE"; then
  record "sha256_windows" PASS
else
  record "sha256_windows" FAIL "mismatch"
fi

phase "4 — Unpack layout (Linux + Windows portable)"
EXTRACT="$WORKDIR/linux-extract"
mkdir -p "$EXTRACT"
tar -xzf "$LINUX_TGZ" -C "$EXTRACT"
LINUX_DIR="$EXTRACT/linux"
for f in hackme pool.miner.token start_hackme_miner.sh setup_hackme_miner.sh workerpoh; do
  [[ -e "$LINUX_DIR/$f" ]] && record "linux_$f" PASS || record "linux_$f" FAIL "missing"
done
[[ -x "$LINUX_DIR/hackme" ]] && record "linux_hackme_exec" PASS || record "linux_hackme_exec" FAIL

WIN_EX="$WORKDIR/win-extract"
mkdir -p "$WIN_EX"
unzip -q "$WIN_ZIP" -d "$WIN_EX"
for f in hackme.exe pool.miner.token start_hackme_miner.bat setup_hackme_miner.bat workerpoh.exe; do
  [[ -f "$WIN_EX/$f" ]] && record "windows_$f" PASS || record "windows_$f" FAIL "missing"
done

POOL_LEN="$(wc -c <"$LINUX_DIR/pool.miner.token" | tr -d ' ')"
if [[ "$POOL_LEN" -gt 20 ]]; then
  record "pool_miner_token" PASS "len=$POOL_LEN"
else
  record "pool_miner_token" FAIL "len=$POOL_LEN"
fi

ISO_LOCAL="$LOCAL_DIST/HackMe-OS-${VERSION}-amd64.iso"
if [[ -f "$ISO_LOCAL" ]]; then
  if bash "$ROOT/scripts/tests/verify_hackme_iso.sh" "$ISO_LOCAL" >>"$OUT/iso.log" 2>&1; then
    record "iso_verify" PASS "local ISO"
  else
    record "iso_verify" FAIL "verify_hackme_iso"
  fi
else
  ISO_LEN="$(curl -sSI "$DIST_URL/HackMe-OS-${VERSION}-amd64.iso" | awk 'BEGIN{IGNORECASE=1} /^content-length:/ {print $2}' | tr -d '\r')"
  if [[ -n "${ISO_LEN:-}" && "$ISO_LEN" -gt 800000000 ]]; then
    record "iso_cdn_size" PASS "bytes=$ISO_LEN"
  else
    record "iso_cdn_size" FAIL "size=${ISO_LEN:-?}"
  fi
fi

phase "5 — One-click setup (Linux, no long daemon if skipped)"
if [[ "${SKIP_LIVE_MINER_START:-0}" == "1" ]]; then
  record "linux_setup_scripts" PASS "skipped live start (port 8080 in use or SKIP_LIVE_MINER_START=1)"
  [[ -x "$LINUX_DIR/setup_hackme_miner.sh" ]] && record "setup_script_present" PASS || record "setup_script_present" FAIL
else
  cd "$LINUX_DIR"
  bash setup_hackme_miner.sh >>"$OUT/setup.log" 2>&1
  [[ -f .env ]] && record "setup_wrote_env" PASS || record "setup_wrote_env" FAIL
  HACKME_MINER_DAEMON=1 bash start_hackme_miner.sh >>"$OUT/start.log" 2>&1
  sleep 8
  if curl -fsS http://127.0.0.1:8080/api/status >/dev/null 2>&1; then
    record "local_node_status" PASS ":8080"
  else
    record "local_node_status" FAIL ":8080"
    tail -n 15 logs/hackme-node.log >>"$OUT/start.log" 2>/dev/null || true
  fi
  if [[ -f logs/hackme-node.pid ]]; then
    kill "$(cat logs/hackme-node.pid)" 2>/dev/null || true
  fi
  pkill -f "$LINUX_DIR/hackme" 2>/dev/null || true
fi

phase "6 — Prod pool (coordinator, no extra GPU worker)"
code="$(curl -sS -o "$OUT/coord_stats.json" -w '%{http_code}' --max-time 25 "${COORD%/}/api/work/stats" || echo 000)"
if [[ "$code" == "200" ]]; then
  record "prod_coord_stats" PASS "HTTP $code"
  jq -c '{workers_count,accepted_attempts,submitted_items}' "$OUT/coord_stats.json" >"$OUT/coord_snapshot.json" 2>/dev/null || true
else
  record "prod_coord_stats" FAIL "HTTP $code"
fi

code="$(curl -sS -o "$OUT/node_status.json" -w '%{http_code}' --max-time 25 "${SITE%/}/api/status" || echo 000)"
if [[ "$code" == "200" ]]; then
  record "prod_api_status" PASS
  tip="$(jq -r '.display_tip_height // .tip_height // empty' "$OUT/node_status.json" 2>/dev/null || true)"
  record "prod_tip_height" PASS "tip=${tip:-?}"
else
  record "prod_api_status" FAIL "HTTP $code"
fi

if [[ "${SKIP_WORKER_SMOKE:-1}" != "1" ]]; then
  phase "7 — Optional GPU worker smoke (isolated WORKER_ID)"
  export COORD_URL="$COORD" WORKER_ID="${WORKER_ID:-wow-journey-$STAMP}" PUBLIC_WORKER_SMOKE_SEC="${PUBLIC_WORKER_SMOKE_SEC:-45}"
  if bash "$ROOT/scripts/ops/run_public_worker_smoke.sh" >>"$OUT/worker_smoke.log" 2>&1; then
    record "prod_worker_smoke" PASS "$WORKER_ID"
  else
    ec=$?
    if [[ "$ec" -eq 0 ]]; then
      record "prod_worker_smoke" PASS "skip no secrets"
    else
      record "prod_worker_smoke" FAIL "exit $ec"
    fi
  fi
else
  record "prod_worker_smoke" PASS "skipped (SAFE — keep worker-kapa-pc / overnight)"
fi

verdict="GO"
if [[ "$failures" -gt 0 ]]; then
  verdict="NO-GO"
elif [[ "$passes" -lt 10 ]]; then
  verdict="WARN"
fi

{
  echo "# Miner journey WOW — $STAMP"
  echo ""
  echo "**Verdict:** $verdict · **pass** $passes · **fail** $failures"
  echo ""
  echo "| Phase |"
  echo "|-------|"
  for p in "${PHASE_LOG[@]}"; do
    echo "| $p |"
  done
  echo ""
  echo "Site: $SITE · Version: $VERSION · Coordinator: $COORD"
  echo ""
  echo "Checks: \`checks.jsonl\` · Full JSON: \`report.json\`"
} >"$REPORT_MD"

python3 - "$REPORT_JSON" "$STAMP" "$SITE" "$VERSION" "$verdict" "$passes" "$failures" "$OUT/checks.jsonl" <<'PY'
import json, sys
from pathlib import Path
out_json, stamp, site, version, verdict, passes, failures, checks_path = sys.argv[1:9]
checks = []
p = Path(checks_path)
if p.exists():
    for line in p.read_text().splitlines():
        line = line.strip()
        if line:
            checks.append(json.loads(line))
Path(out_json).write_text(json.dumps({
    "run_id": stamp,
    "verdict": verdict,
    "site": site,
    "version": version,
    "passes": int(passes),
    "failures": int(failures),
    "checks": checks,
}, indent=2) + "\n")
PY

echo ""
echo "[wow] $verdict — $passes pass / $failures fail"
echo "[wow] $REPORT_MD"
echo "[wow] $REPORT_JSON"

if [[ "$failures" -gt 0 ]]; then
  exit 1
fi
exit 0
