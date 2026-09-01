#!/usr/bin/env bash
# Gate: libFuzzer seed import → Hunt L2 corpus merge.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

go test -count=1 ./internal/hunt -run 'TestMergeLibFuzzerSeedCorpus|TestApplyHuntPowerScheduling|TestExportLibFuzzerSeeds|TestLoadLibFuzzerSeedFilesFiltersCrash|TestDedicatedLibFuzzerHarness|TestRunLibFuzzerImportSessionSynthetic' -timeout 3m

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
TARGET="gate-target"
SEED_DIR="$TMP/.cache/hunt-lf-seeds/$TARGET"
mkdir -p "$SEED_DIR"
printf '%s' '{"a":1}{"a":1}' >"$SEED_DIR/known.bin"

out="$(HACKME_REPO_ROOT="$TMP" go run "$ROOT/scripts/tests/tools/hunt_merge_seeds_check.go" -repo "$TMP" -target "$TARGET")"
python3 - "$out" <<'PY'
import json, sys
doc = json.loads(sys.argv[1])
assert doc.get("merged") == 1, doc
assert doc.get("guided") is True, doc
print("merge ok", doc)
PY

echo "[hunt-corpus-import-gate] PASS"
