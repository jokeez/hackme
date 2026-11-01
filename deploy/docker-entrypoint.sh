#!/usr/bin/env bash
# OOTB docker entry: miner seed + hybrid Dig/PoH env; never clobber node_ed25519.seed.
set -euo pipefail
export PATH="/opt/hackme:/opt/hackme/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"

DATA_DIR="${HACKME_DATA_DIR:-/data}"
mkdir -p "$DATA_DIR" "$DATA_DIR/.secrets" /opt/hackme/logs /opt/hackme/bin

# Writable generated miner seed (NOT node_ed25519.seed — that file is hex-only, owned by the node).
GEN_SEED_FILE="${HACKME_MINER_SEED_HEX_FILE:-$DATA_DIR/.secrets/miner.seed.hex}"
NODE_SEED_FILE="${DATA_DIR}/node_ed25519.seed"

if [[ -z "${HACKME_MINER_ED25519_SEED_HEX:-}" ]]; then
  if [[ -f "$GEN_SEED_FILE" ]]; then
    export HACKME_MINER_ED25519_SEED_HEX="$(tr -d '[:space:]' <"$GEN_SEED_FILE")"
  elif [[ -f "$NODE_SEED_FILE" ]]; then
    # Node already created wallet seed — reuse for hybrid submits (desktop unified).
    export HACKME_MINER_ED25519_SEED_HEX="$(tr -d '[:space:]' <"$NODE_SEED_FILE")"
    export HACKME_MINER_SEED_FILE="${HACKME_MINER_SEED_FILE:-$NODE_SEED_FILE}"
  elif command -v minersign >/dev/null 2>&1; then
    raw="$(minersign -gen-seed 2>/dev/null || true)"
    hex="$(printf '%s' "$raw" | sed -n 's/.*"HACKME_MINER_ED25519_SEED_HEX"[[:space:]]*:[[:space:]]*"\([0-9a-fA-F]\{64\}\)".*/\1/p')"
    if [[ -z "$hex" ]]; then
      hex="$(printf '%s' "$raw" | tr -d '[:space:]' | grep -Eo '[0-9a-fA-F]{64}' | head -1 || true)"
    fi
    if [[ ${#hex} -eq 64 ]]; then
      printf '%s\n' "$hex" >"$GEN_SEED_FILE"
      chmod 600 "$GEN_SEED_FILE"
      export HACKME_MINER_ED25519_SEED_HEX="$hex"
      echo "[entrypoint] generated miner seed → $GEN_SEED_FILE"
    fi
  fi
fi

# Prefer node wallet seed path for LoadHybridKey once the node creates it.
if [[ -z "${HACKME_MINER_SEED_FILE:-}" ]]; then
  export HACKME_MINER_SEED_FILE="$NODE_SEED_FILE"
fi

if [[ -n "${HACKME_MINER_ED25519_SEED_HEX:-}" ]]; then
  export HACKME_WORKER_SIGN_SUBMITS="${HACKME_WORKER_SIGN_SUBMITS:-1}"
fi

if [[ -x /opt/hackme/bin/hackme-wasm-check ]]; then
  export HACKME_WASM_VALIDATE_BIN="${HACKME_WASM_VALIDATE_BIN:-/opt/hackme/bin/hackme-wasm-check}"
fi

export HACKME_WORKER_HYBRID_FUZZ_MODE="${HACKME_WORKER_HYBRID_FUZZ_MODE:-inline}"
export HACKME_WORKERFUZZ_BIN="${HACKME_WORKERFUZZ_BIN:-/opt/hackme/bin/workerfuzz}"

# bwrap: keep STRICT on when probe works; docker often lacks userns → fail soft.
if command -v bwrap >/dev/null 2>&1; then
  if bwrap --ro-bind / / --dev /dev --proc /proc --unshare-pid --unshare-net --die-with-parent -- \
      "$(command -v true 2>/dev/null || echo /bin/true)" >/dev/null 2>&1; then
    export HACKME_HUNT_ISOLATE_STRICT="${HACKME_HUNT_ISOLATE_STRICT:-1}"
    export HACKME_HUNT_BUILD_SANDBOX="${HACKME_HUNT_BUILD_SANDBOX:-1}"
  else
    echo "[entrypoint] WARN: bwrap probe failed — STRICT/BUILD_SANDBOX=0 for this container"
    export HACKME_HUNT_ISOLATE_STRICT="${HACKME_HUNT_ISOLATE_STRICT:-0}"
    export HACKME_HUNT_BUILD_SANDBOX="${HACKME_HUNT_BUILD_SANDBOX:-0}"
  fi
else
  echo "[entrypoint] WARN: bwrap missing — STRICT/BUILD_SANDBOX=0"
  export HACKME_HUNT_ISOLATE_STRICT="${HACKME_HUNT_ISOLATE_STRICT:-0}"
  export HACKME_HUNT_BUILD_SANDBOX="${HACKME_HUNT_BUILD_SANDBOX:-0}"
fi

cd "${HACKME_WORKING_DIR:-/opt/hackme}"
exec "$@"
