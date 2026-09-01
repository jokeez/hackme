#!/usr/bin/env bash
# Gate: Hunt L2 libFuzzer seeds — merge smoke + optional live import A/B on jsmn.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

echo "[hunt-l2-seeds-ab-gate] unit tests"
go test -count=1 ./internal/hunt -run 'TestMergeLibFuzzerSeedCorpus|TestApplyHuntPowerScheduling|TestExportLibFuzzerSeeds|TestLoadLibFuzzerSeedFiles|TestLibFuzzerSeedDir' -timeout 2m
go test -count=1 ./internal/poolfuzz -run 'TestHuntL2LibFuzzerSeedBootstrap' -timeout 2m

echo "[hunt-l2-seeds-ab-gate] merge smoke"
bash "$ROOT/scripts/tests/hunt_corpus_import_gate.sh"

TARGET="${TARGET:-jsmn}"
WALL_SEC="${WALL_SEC:-25}"
if command -v clang >/dev/null 2>&1; then
  echo "[hunt-l2-seeds-ab-gate] live import TARGET=$TARGET wall=${WALL_SEC}s"
  TMP_SEED="$ROOT/.cache/hunt-lf-seeds/${TARGET}.gate-backup"
  if [[ -d "$ROOT/.cache/hunt-lf-seeds/$TARGET" ]]; then
    rm -rf "$TMP_SEED"
    mv "$ROOT/.cache/hunt-lf-seeds/$TARGET" "$TMP_SEED"
  fi
  cleanup() {
    rm -rf "$ROOT/.cache/hunt-lf-seeds/$TARGET"
    if [[ -d "$TMP_SEED" ]]; then
      mv "$TMP_SEED" "$ROOT/.cache/hunt-lf-seeds/$TARGET"
    fi
  }
  trap cleanup EXIT

  before="$(HACKME_REPO_ROOT="$ROOT" go run "$ROOT/scripts/tests/tools/hunt_merge_seeds_check.go" -repo "$ROOT" -target "$TARGET")"
  n="$(TARGET="$TARGET" WALL_SEC="$WALL_SEC" bash "$ROOT/scripts/ops/hunt_import_libfuzzer_corpus.sh")"
  after="$(HACKME_REPO_ROOT="$ROOT" go run "$ROOT/scripts/tests/tools/hunt_merge_seeds_check.go" -repo "$ROOT" -target "$TARGET")"
  python3 - "$before" "$after" "$n" <<'PY'
import json, sys
before, after, n = json.loads(sys.argv[1]), json.loads(sys.argv[2]), int(sys.argv[3])
assert before.get("merged", 0) == 0, before
assert after.get("merged", 0) > 0, after
assert after.get("guided") is True, after
assert n > 0, n
print("live import ok", {"seeds": n, "merged": after["merged"]})
PY
else
  echo "[hunt-l2-seeds-ab-gate] skip live import (no clang)"
fi

echo "[hunt-l2-seeds-ab-gate] PASS"
