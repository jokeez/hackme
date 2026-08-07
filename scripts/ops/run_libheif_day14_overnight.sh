#!/usr/bin/env bash
# Day 14 libheif overnight fuzz until ~15:00 local publish (+1h buffer).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
MAX_TIME="$(python3 - <<'PY'
from datetime import datetime, timedelta
now = datetime.now().astimezone()
target = now.replace(hour=15, minute=0, second=0, microsecond=0)
if target <= now:
    target += timedelta(days=1)
# +1h buffer after 15:00 window, cap 20h
print(min(72000, max(int((target - now).total_seconds()) + 3600, 40000)))
PY
)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)-d14"
export DAY=14 MAX_TIME STAMP SKIP_REBUILD=1 RSS_LIMIT_MB="${RSS_LIMIT_MB:-2048}"
echo "[day14-overnight] DAY=14 MAX_TIME=${MAX_TIME}s STAMP=$STAMP"
exec bash "$ROOT/scripts/ops/run_oss_cve_watch_libheif_libfuzzer_day.sh"
