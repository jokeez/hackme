#!/usr/bin/env bash
set -euo pipefail

# code_quality_audit.sh
# Lightweight static quality audit:
# - duplicate source file detection
# - single embedded dashboard (dashboard.html only; former vps twin removed)
# - duplicate large chunk signal (warning-only)
#
# Usage:
#   bash scripts/ops/code_quality_audit.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[code-quality-audit] missing command: $1" >&2
    exit 1
  }
}

require_cmd python3
require_cmd jq

RUN_ID="${RUN_ID:-code_quality_audit_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/gates/$RUN_ID}"
mkdir -p "$OUT_DIR"

SUMMARY_JSON="$OUT_DIR/summary.json"

python3 - "$ROOT_DIR" "$SUMMARY_JSON" <<'PY'
import collections
import hashlib
import json
import os
import re
import sys
from pathlib import Path

root = Path(sys.argv[1])
summary_path = Path(sys.argv[2])

skip_dirs = {
    ".git", ".cursor", ".cache", "bin", "dist", "data", "logs", "tmp", "node_modules",
    "reports", "__pycache__", "data_backup_20260426_203131", "data_tmp_20260426_203243"
}
source_ext = {".go", ".sh", ".md", ".html", ".js", ".ts", ".css", ".json", ".yml", ".yaml"}
def walk_source_files():
    for dp, dns, fns in os.walk(root):
        dns[:] = [d for d in dns if d not in skip_dirs]
        for fn in fns:
            p = Path(dp) / fn
            rel = p.relative_to(root).as_posix()
            if any(part in rel.split("/") for part in skip_dirs):
                continue
            if p.suffix.lower() not in source_ext:
                continue
            yield p, rel

files = list(walk_source_files())

# 1) Exact duplicate source files
by_hash = collections.defaultdict(list)
for p, rel in files:
    try:
        b = p.read_bytes()
    except Exception:
        continue
    if len(b) < 64:
        continue
    by_hash[hashlib.sha256(b).hexdigest()].append(rel)

dup_groups = []
unexpected_dups = []
for group in by_hash.values():
    if len(group) <= 1:
        continue
    group_sorted = sorted(group)
    dup_groups.append(group_sorted)
    unexpected_dups.append(group_sorted)

# 2) Embedded dashboard present
dash = root / "dashboard.html"
dashboard_hash = {}
if dash.exists():
    h1 = hashlib.sha256(dash.read_bytes()).hexdigest()
    dashboard_hash = {"dashboard_html_sha256": h1}

# 3) Duplicate large chunks warning (non-blocking)
chunk_groups = collections.defaultdict(list)
split_pat = re.compile(r"\n\s*\n+", flags=re.M)
for p, rel in files:
    try:
        s = p.read_text(encoding="utf-8", errors="ignore")
    except Exception:
        continue
    for chunk in split_pat.split(s):
        c = chunk.strip()
        if len(c) < 220:
            continue
        key = hashlib.sha1(c.encode("utf-8")).hexdigest()
        chunk_groups[key].append(rel)

dup_chunk_groups = [sorted(set(v)) for v in chunk_groups.values() if len(set(v)) > 1]
dup_chunk_groups.sort(key=lambda v: (-len(v), v))

warnings = []
if dup_chunk_groups:
    # Cap to keep summary compact.
    top = dup_chunk_groups[:20]
    warnings.append({
        "kind": "duplicate_large_chunks",
        "groups_count": len(dup_chunk_groups),
        "top_groups": top,
    })
fail_reasons = []
if unexpected_dups:
    fail_reasons.append("unexpected_exact_duplicate_source_files")

summary = {
    "gate": "code_quality_audit_v1",
    "pass": len(fail_reasons) == 0,
    "fail_reasons": fail_reasons,
    "metrics": {
        "source_files_scanned": len(files),
        "exact_duplicate_groups": len(dup_groups),
        "unexpected_exact_duplicate_groups": len(unexpected_dups),
        "dashboard_html_present": dash.exists(),
    },
    "details": {
        "unexpected_exact_duplicate_groups": unexpected_dups,
        "dashboard_hash": dashboard_hash,
    },
    "warnings": warnings,
}

summary_path.write_text(json.dumps(summary, indent=2), encoding="utf-8")
print(json.dumps(summary, indent=2))
sys.exit(0 if summary["pass"] else 1)
PY

echo "[code-quality-audit] summary: $SUMMARY_JSON"
