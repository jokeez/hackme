#!/usr/bin/env bash
# Apply production security env on VPS (coordinator + node). Idempotent.
# Usage: NODE_SSH=hackme-vps bash scripts/ops/apply_security_hardening_vps.sh
set -euo pipefail

NODE_SSH="${NODE_SSH:-hackme-vps}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"

ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
DEPLOY='$DEPLOY'
set_kv() {
  local f="\$1" k="\$2" v="\$3"
  touch "\$f"
  if grep -q "^\${k}=" "\$f" 2>/dev/null; then
    sed -i "s|^\${k}=.*|\${k}=\${v}|" "\$f"
  else
    echo "\${k}=\${v}" >>"\$f"
  fi
}
COORD="\$DEPLOY/.env.coord"
VPS="\$DEPLOY/.env.vps"
set_kv "\$COORD" HACKME_COORDINATOR_ADDR "127.0.0.1:18081"
set_kv "\$COORD" HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN "1"
set_kv "\$COORD" HACKME_COORDINATOR_PAYOUT_FOUND_ONLY "1"
set_kv "\$COORD" HACKME_POOL_HYBRID_SIGNER_ENABLED "1"
set_kv "\$COORD" HACKME_POOL_HYBRID_SIGNER_STRICT "1"
set_kv "\$COORD" HACKME_POOL_HYBRID_REQUIRE_FOUND_SIG "1"
grep -q '^HACKME_COORDINATOR_ALLOW_INSECURE=' "\$COORD" 2>/dev/null && \
  sed -i '/^HACKME_COORDINATOR_ALLOW_INSECURE=/d' "\$COORD" || true
set_kv "\$VPS" HACKME_REQUIRE_ADMIN_TOKEN "1"
set_kv "\$VPS" HACKME_BIND_ADDR "127.0.0.1:18080"
grep -q '^HACKME_COORDINATOR_ADMIN_TOKEN=' "\$COORD" || {
  echo "ERROR: HACKME_COORDINATOR_ADMIN_TOKEN missing in \$COORD" >&2
  exit 1
}
grep -q '^HACKME_ADMIN_TOKEN=' "\$VPS" || {
  echo "ERROR: HACKME_ADMIN_TOKEN missing in \$VPS" >&2
  exit 1
}
echo "[hardening] coordinator env:"
grep -E '^(HACKME_COORDINATOR_|HACKME_POOL_HYBRID_)' "\$COORD" | sed 's/=.*/=***/'
echo "[hardening] node require_admin:"
grep -E '^HACKME_REQUIRE_ADMIN_TOKEN=' "\$VPS" | sed 's/=.*/=***/'
REMOTE

echo "[hardening] done (restart services via deploy_hackme_node.sh)"
