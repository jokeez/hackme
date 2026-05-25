#!/usr/bin/env bash
# One-shot: node + developer token + nginx/site + fuzzing CLI binaries + smoke.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
NODE_SSH="${NODE_SSH:-hackme-vps}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
export NODE_SSH NODE_DEPLOY_DIR

bash scripts/ops/build_fuzzing_client.sh
bash scripts/ops/deploy_hackme_node.sh
bash scripts/ops/vps_ensure_integrator_env.sh
SYNC_NGINX_SITE_CONF=1 bash scripts/ops/deploy_hackme_site.sh
bash scripts/tests/fuzzing_public_hardening_smoke.sh
bash scripts/tests/fuzzing_developer_portal_smoke.sh
bash scripts/tests/integrator_self_service_smoke.sh
echo "[deploy-fuzzing-b2b] done — post Telegram: docs/TELEGRAM_POST_FUZZING_B2B.txt"
echo "[deploy-fuzzing-b2b] Bitcointalk: docs/BITCOINTALK_UPDATE_FUZZING_B2B_BBCode.txt"
