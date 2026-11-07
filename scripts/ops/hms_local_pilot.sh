#!/usr/bin/env bash
# Wrapper — pilot lives under scripts/local/ops (kept out of hub deploy trees).
exec bash "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/scripts/local/ops/hms_local_pilot.sh" "$@"
