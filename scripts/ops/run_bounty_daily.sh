#!/usr/bin/env bash
# Daily bounty sweep — delegates to autopilot (fast budgets).
#
#   bash scripts/ops/run_bounty_daily.sh
#   FAST=0 bash scripts/ops/run_bounty_daily.sh   # full autopilot (hours)
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export FAST="${FAST:-1}"
export SKIP_PHASES="${SKIP_PHASES:-}"
exec bash "$ROOT/scripts/ops/run_bounty_autopilot.sh"
