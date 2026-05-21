#!/usr/bin/env bash
# Dry-run HackMe OS tuning on the current Linux host (no ISO required).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
export HACKME_ROOT="$ROOT"
sudo mkdir -p /run/hackme-os /var/lib/hackme
sudo HACKME_ROOT="$ROOT" bash "$ROOT/scripts/release/iso/hackme-os-tune.sh"
sudo HACKME_ROOT="$ROOT" bash "$ROOT/scripts/release/iso/hackme-os-gpu-tune.sh"
sudo HACKME_ROOT="$ROOT" bash "$ROOT/scripts/release/iso/hackme-os-rig-profile.sh"
echo "--- topology ---"
cat /run/hackme-os/topology.json 2>/dev/null || true
echo "--- rig ---"
cat /var/lib/hackme/rig.env 2>/dev/null || true
echo "Run 30s bench: sudo HACKME_ROOT=$ROOT bash scripts/release/iso/hackme-os-benchmark.sh 30"
