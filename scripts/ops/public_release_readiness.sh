#!/usr/bin/env bash
set -euo pipefail
#
# Быстрый срез готовности к публичному запуску (без полного дублирования repo_final_selfcheck).
# Полный прогон перед merge: bash scripts/ops/repo_final_selfcheck.sh
#
# Опции:
#   RUN_FULL_GO_TEST=1     — go test ./... -count=1 без -short (долго)
#   PUBLIC_RO_BASE=URL     — read-only GET /api/status, /api/global/metrics, worker/settlement
#
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[public-readiness] missing: $1" >&2
    exit 1
  }
}

require_cmd go
require_cmd bash
require_cmd curl
require_cmd jq
require_cmd python3

echo "[public-readiness] gofmt (must be clean)"
if [[ -n "$(gofmt -l . 2>/dev/null || true)" ]]; then
  gofmt -l . >&2 || true
  echo "[public-readiness] FAIL: gofmt -l — run gofmt -w" >&2
  exit 1
fi

echo "[public-readiness] go vet ./..."
go vet ./...

RUN_FULL="${RUN_FULL_GO_TEST:-0}"
if [[ "$RUN_FULL" == "1" ]]; then
  echo "[public-readiness] go test ./... -count=1 (full)"
  go test ./... -count=1
else
  echo "[public-readiness] go test ./... -short -count=1 (RUN_FULL_GO_TEST=1 for full suite)"
  go test ./... -short -count=1
fi

echo "[public-readiness] go build -trimpath ./..."
go build -trimpath -o /dev/null ./...

echo "[public-readiness] web/site/assets/news.json (JSON)"
python3 -m json.tool web/site/assets/news.json >/dev/null

PUBLIC_RO_BASE="${PUBLIC_RO_BASE:-}"
if [[ -n "$PUBLIC_RO_BASE" ]]; then
  echo "[public-readiness] PUBLIC_RO_BASE=$PUBLIC_RO_BASE (read-only)"
  curl -fsS --max-time 20 "${PUBLIC_RO_BASE%/}/api/status" | jq -e '.has_genesis == true' >/dev/null
  curl -fsS --max-time 20 "${PUBLIC_RO_BASE%/}/api/global/metrics" | jq -e '.ok == true' >/dev/null
  code_settle="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 20 "${PUBLIC_RO_BASE%/}/api/worker/settlement" || true)"
  if [[ "$code_settle" != "200" ]]; then
    echo "[public-readiness] WARN: GET .../api/worker/settlement -> HTTP $code_settle" >&2
  else
    curl -fsS --max-time 20 "${PUBLIC_RO_BASE%/}/api/worker/settlement" | jq -e 'has("ok")' >/dev/null
  fi
  echo "[public-readiness] public RO probes OK"
fi

echo ""
echo "[public-readiness] VERDICT: автоматический срез репозитория — PASS"
echo "[public-readiness] Дальше только операторский хвост: см. docs/PUBLIC_LAUNCH_VERDICT.md и docs/OPERATOR_FINAL_CHECKLIST.md"
exit 0
