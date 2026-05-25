#!/usr/bin/env bash
set -euo pipefail
#
# One-shot repo checks before release / merge (no live stack, no secrets).
# Optional read-only probes against a public command node:
#   PUBLIC_RO_BASE=https://hackme.tech bash scripts/ops/repo_final_selfcheck.sh
#
# Optional deeper checks:
#   RUN_LOCAL_STACK_SMOKE=1  — ephemeral coordinator+node+worker (~30s, needs rsync, timeout)
#   RUN_PUBLIC_WORKER_SMOKE=1 — live claim/submit vs https://hackme.tech/pool/coordinator (~40s)
#       if .secrets/hackme_coordinator_admin_token exists (VPS HACKME_COORDINATOR_ADMIN_TOKEN); else SKIP
#   RUN_SHELLCHECK=1       — if shellcheck is installed, lint this script + top_pool gate
#
# If ADMIN_TOKEN / HACKME_ADMIN_TOKEN are unset and `.secrets/hackme_admin_token` exists (first line),
# it is exported for smoke only — never printed.
#
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

if [[ -z "${ADMIN_TOKEN:-}" && -z "${HACKME_ADMIN_TOKEN:-}" && -f "$ROOT_DIR/.secrets/hackme_admin_token" ]]; then
  _t="$(head -n1 "$ROOT_DIR/.secrets/hackme_admin_token" | tr -d '\r\n')"
  if [[ -n "$_t" ]]; then
    export ADMIN_TOKEN="$_t"
  fi
  unset _t
fi

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[repo-final-selfcheck] missing: $1" >&2
    exit 1
  }
}

require_cmd go
require_cmd bash
require_cmd curl
require_cmd jq
require_cmd python3

RUN_LOCAL_STACK_SMOKE="${RUN_LOCAL_STACK_SMOKE:-0}"
RUN_SHELLCHECK="${RUN_SHELLCHECK:-0}"

echo "[repo-final-selfcheck] cleanup stale .go under reports/ (breaks go test ./...)"
find reports -type f \( -name 'sign_transfer_tmp.go' -o -name '_sign_transfer.go' \) -delete 2>/dev/null || true

echo "[repo-final-selfcheck] gofmt (must be clean)"
if [[ -n "$(gofmt -l . 2>/dev/null || true)" ]]; then
  gofmt -l . >&2 || true
  echo "[repo-final-selfcheck] ERROR: gofmt -l reported files above; run: gofmt -w on those paths" >&2
  exit 1
fi

echo "[repo-final-selfcheck] go vet ./..."
go vet ./...

echo "[repo-final-selfcheck] go test ./... -count=1"
go test ./... -count=1

echo "[repo-final-selfcheck] go build -trimpath ./... (all packages)"
go build -trimpath -o /dev/null ./...

echo "[repo-final-selfcheck] code_quality_audit"
bash scripts/ops/code_quality_audit.sh

echo "[repo-final-selfcheck] bash -n scripts/ops/*.sh scripts/lib/*.sh"
while IFS= read -r -d '' f; do
  bash -n "$f" || {
    echo "[repo-final-selfcheck] ERROR: bash -n failed: $f" >&2
    exit 1
  }
done < <(find scripts/ops scripts/lib -maxdepth 1 -type f -name '*.sh' -print0 2>/dev/null || true)

if [[ "$RUN_SHELLCHECK" == "1" ]] && command -v shellcheck >/dev/null 2>&1; then
  echo "[repo-final-selfcheck] shellcheck (subset)"
  shellcheck -x "$ROOT_DIR/scripts/ops/repo_final_selfcheck.sh" \
    "$ROOT_DIR/scripts/ops/top_pool_readiness_gate.sh" \
    "$ROOT_DIR/scripts/ops/use_hackme_tech_public_authority_env.sh" || exit 1
elif [[ "$RUN_SHELLCHECK" == "1" ]]; then
  echo "[repo-final-selfcheck] WARN: RUN_SHELLCHECK=1 but shellcheck not in PATH" >&2
fi

OUT_BIN="${OUT_BIN:-${TMPDIR:-/tmp}/hackme-selfcheck-$(date -u +%Y%m%dT%H%M%SZ)}"
echo "[repo-final-selfcheck] go build -trimpath (main binary) -> $OUT_BIN"
go build -trimpath -o "$OUT_BIN" .
rm -f "$OUT_BIN"

if [[ "$RUN_LOCAL_STACK_SMOKE" == "1" ]]; then
  require_cmd rsync
  require_cmd timeout
  echo "[repo-final-selfcheck] _local_stack_smoke (RUN_LOCAL_STACK_SMOKE=1)"
  SMOKE_WORKER_SEC="${SMOKE_WORKER_SEC:-15}" bash scripts/ops/_local_stack_smoke.sh
fi

PUBLIC_RO_BASE="${PUBLIC_RO_BASE:-}"
if [[ -n "$PUBLIC_RO_BASE" ]]; then
  echo "[repo-final-selfcheck] PUBLIC_RO_BASE=$PUBLIC_RO_BASE (read-only)"
  curl -fsS --max-time 20 "${PUBLIC_RO_BASE%/}/api/status" | jq -e '.has_genesis == true' >/dev/null
  curl -fsS --max-time 20 "${PUBLIC_RO_BASE%/}/api/global/metrics" | jq -e '.ok == true' >/dev/null
  code_settle="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 20 "${PUBLIC_RO_BASE%/}/api/worker/settlement" || true)"
  if [[ "$code_settle" != "200" ]]; then
    echo "[repo-final-selfcheck] WARN: GET .../api/worker/settlement -> HTTP $code_settle (update nginx allowlist: worker/settlement)" >&2
  else
    curl -fsS --max-time 20 "${PUBLIC_RO_BASE%/}/api/worker/settlement" | jq -e 'has("ok")' >/dev/null
  fi
  echo "[repo-final-selfcheck] public RO probes OK"
fi

if [[ "${RUN_PUBLIC_WORKER_SMOKE:-0}" == "1" ]]; then
  require_cmd timeout
  echo "[repo-final-selfcheck] run_public_worker_smoke (RUN_PUBLIC_WORKER_SMOKE=1)"
  bash "$ROOT_DIR/scripts/ops/run_public_worker_smoke.sh"
fi

echo "[repo-final-selfcheck] PASS"
