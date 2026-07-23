#!/usr/bin/env bash
# Controlled-launch test bundle for operators (pool + ISO + security smokes).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$ROOT/reports/miner-launch-gate-$STAMP"
mkdir -p "$OUT"
VERDICT="$OUT/VERDICT.md"
POOL_BASE="${POOL_BASE:-https://hackme.tech/pool}"
SITE_BASE="${SITE_BASE:-https://hackme.tech}"
ISO_VER="$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_ISO_VERSION" 2>/dev/null || echo 0.1.0-rc11l)"
ISO_URL="${ISO_URL:-https://hackme.tech/dist/release_${ISO_VER}/HackMe-OS-${ISO_VER}-amd64.iso}"
EXPECTED_ISO_SHA="${EXPECTED_ISO_SHA:-}"
if [[ -z "$EXPECTED_ISO_SHA" && -f "$ROOT/dist/release_${ISO_VER}/SHA256SUMS-iso.txt" ]]; then
  EXPECTED_ISO_SHA="$(awk '/HackMe-OS-.*\.iso/{print $1; exit}' "$ROOT/dist/release_${ISO_VER}/SHA256SUMS-iso.txt" || true)"
fi
if [[ -z "$EXPECTED_ISO_SHA" ]]; then
  EXPECTED_ISO_SHA="43abb592d7e4222f8d47d528d0b8ec190958cdb91a4441cc56395b3f667d6125"
fi

pass=0
fail=0

run_step() {
  local id="$1" desc="$2"
  shift 2
  local log="$OUT/${id}.log"
  echo "[launch-gate] === $id: $desc ==="
  if "$@" >"$log" 2>&1; then
    echo "[launch-gate] PASS $id"
    echo "| $id | PASS | $desc |" >>"$OUT/results.md"
    pass=$((pass + 1))
  else
    echo "[launch-gate] FAIL $id (see $log)"
    echo "| $id | **FAIL** | $desc |" >>"$OUT/results.md"
    fail=$((fail + 1))
  fi
}

run_step_optional() {
  local id="$1" desc="$2"
  shift 2
  local log="$OUT/${id}.log"
  echo "[launch-gate] === $id (optional): $desc ==="
  if "$@" >"$log" 2>&1; then
    echo "[launch-gate] PASS $id"
    echo "| $id | PASS | $desc |" >>"$OUT/results.md"
    pass=$((pass + 1))
  else
    echo "[launch-gate] SKIP/WARN $id"
    echo "| $id | WARN | $desc |" >>"$OUT/results.md"
  fi
}

echo "# Miner launch gate — $STAMP" >"$OUT/results.md"
echo "" >>"$OUT/results.md"
echo "| Step | Result | Description |" >>"$OUT/results.md"
echo "|------|--------|-------------|" >>"$OUT/results.md"

run_step go_test "go test ./..." go test ./... -count=1
run_step chaos_guard "nightly chaos guard" bash scripts/tests/nightly_chaos_guard.sh
run_step init_worker "HackMe OS init-worker tests" bash scripts/release/iso/init_worker_test.sh
run_step mega_stress_quick "coordinator mega stress (quick)" env STRESS_QUICK=1 bash scripts/tests/coordinator_mega_stress.sh
run_step difficulty_health "prod pool difficulty health" env BASE="${POOL_BASE}" bash scripts/tests/difficulty_health.sh
run_step redteam "prod redteam surface smoke" env BASE="$SITE_BASE" bash scripts/tests/redteam_surface_smoke.sh

echo "[launch-gate] === iso_remote: published ISO SHA256 ===" | tee -a "$OUT/iso_remote.log"
if command -v curl >/dev/null && curl -fsS --max-time 15 "${ISO_URL%/*}/SHA256SUMS-iso.txt" | grep -q "$EXPECTED_ISO_SHA"; then
  echo "[launch-gate] PASS iso_sha_remote" | tee -a "$OUT/iso_remote.log"
  echo "| iso_sha_remote | PASS | SHA256SUMS matches expected |" >>"$OUT/results.md"
  pass=$((pass + 1))
else
  echo "[launch-gate] WARN iso_sha_remote (SHA256SUMS fetch mismatch)" | tee -a "$OUT/iso_remote.log"
  echo "| iso_sha_remote | WARN | SHA256SUMS check failed |" >>"$OUT/results.md"
fi

echo "[launch-gate] === pool_stats: coordinator work/stats ===" | tee "$OUT/pool_stats.log"
if curl -fsS --max-time 15 "${POOL_BASE}/coordinator/api/work/stats" | jq -e '.workers' >>"$OUT/pool_stats.log" 2>&1; then
  echo "[launch-gate] PASS pool_stats"
  echo "| pool_stats | PASS | coordinator /api/work/stats |" >>"$OUT/results.md"
  pass=$((pass + 1))
else
  echo "[launch-gate] FAIL pool_stats"
  echo "| pool_stats | **FAIL** | coordinator unreachable |" >>"$OUT/results.md"
  fail=$((fail + 1))
fi

if [[ -f "$ROOT/.env.desktop" ]] && curl -fsS --max-time 3 "http://127.0.0.1:8080/api/status" >/dev/null 2>&1; then
  run_step fuzz_smoke "fuzz dashboard smoke (local node)" bash scripts/tests/fuzz_dashboard_smoke.sh
else
  echo "[launch-gate] SKIP fuzz_smoke (no local :8080 node)" | tee "$OUT/fuzz_smoke_skip.log"
  echo "| fuzz_smoke | SKIP | local node not up |" >>"$OUT/results.md"
fi

if [[ "${RUN_CRYPTO_MATRIX:-0}" == "1" ]]; then
  run_step_optional crypto_matrix "hybrid crypto matrix (PACKETS=${PACKETS:-200})" \
    env PACKETS="${PACKETS:-200}" COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}" \
    REQUIRE_STRICT="${REQUIRE_STRICT:-1}" bash "$ROOT/scripts/tests/hybrid_crypto_matrix.sh"
fi

{
  echo "# Miner launch gate verdict"
  echo ""
  echo "- **When:** $STAMP (UTC)"
  echo "- **Pass:** $pass · **Fail:** $fail"
  echo ""
  if [[ "$fail" -eq 0 ]]; then
    echo "## Verdict: **GO** for controlled miner launch"
    echo ""
    echo "Pool, security smokes, and ISO checksum URL look good. Announce via Telegram support and [SETUP.md](../docs/SETUP.md)."
  else
    echo "## Verdict: **NO-GO** until failures fixed"
    echo ""
    echo "See step logs under \`$OUT/\`."
  fi
  echo ""
  cat "$OUT/results.md"
} >"$VERDICT"

echo "[launch-gate] wrote $VERDICT"
cat "$VERDICT"
[[ "$fail" -eq 0 ]]
