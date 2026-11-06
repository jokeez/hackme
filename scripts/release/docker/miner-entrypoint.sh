#!/usr/bin/env bash
# Container entrypoint: keep all mutable state under /state, start node + pool worker via the
# release scripts, stay in the foreground and stop cleanly on SIGTERM (docker stop).
set -euo pipefail
STATE="${HACKME_STATE_DIR:-/state}"
APP=/opt/hackme
mkdir -p "$STATE/data" "$STATE/logs"
# A pid from the previous container run may now belong to an unrelated process.
rm -f "$STATE/logs/hackme-node.pid"
if [[ ! -w "$STATE/data" ]]; then
  echo "[hackme] $STATE is not writable by uid $(id -u) — use a named volume or chown it to 10001" >&2
  exit 1
fi
# Release scripts expect data/, logs/ and .env next to the binaries.
ln -sfn "$STATE/data" "$APP/data"
ln -sfn "$STATE/logs" "$APP/logs"
if [[ -f "$STATE/.env" ]]; then
  ln -sfn "$STATE/.env" "$APP/.env"
fi
# HACKME_PROFILE=orders: B2B Orders node (Dig/Hunt campaigns, no pool miner). State, env.orders and
# the node key live under the /state volume; the dashboard is on 127.0.0.1:${HACKME_ORDERS_PORT:-8080}.
if [[ "${HACKME_PROFILE:-miner}" == "orders" ]]; then
  export HOME="$STATE" XDG_DATA_HOME="$STATE/share" XDG_CONFIG_HOME="$STATE/config"
  # The image filesystem is thrown away on re-create, so a toolchain download (zig, ~15 min worst
  # case) would repeat on every start and hold the dashboard back. Opt in with SKIP_TOOLCHAINS=0.
  export SKIP_TOOLCHAINS="${SKIP_TOOLCHAINS:-1}"
  if [[ -n "${HACKME_PORT:-}" && -z "${HACKME_ORDERS_PORT:-}" ]]; then
    export HACKME_ORDERS_PORT="$HACKME_PORT"
  fi
  if [[ -f "$STATE/pool.orders.token" ]]; then
    mkdir -p "$XDG_CONFIG_HOME/hackme"
    install -m 0600 "$STATE/pool.orders.token" "$XDG_CONFIG_HOME/hackme/pool.orders.token"
  fi
  OLOG="$XDG_DATA_HOME/hackme/logs/orders"
  rm -f "$OLOG/hackme-orders.pid"
  cd "$APP"
  bash ./start_hackme_orders.sh --start
  PID="$(cat "$OLOG/hackme-orders.pid" 2>/dev/null || true)"
  [[ -n "$PID" ]] || { echo "[hackme] Orders node did not start (see $OLOG)" >&2; sleep 30; exit 1; }
  trap 'echo "[hackme] stopping"; kill -TERM "$PID" 2>/dev/null; for _ in $(seq 1 50); do kill -0 "$PID" 2>/dev/null || break; sleep 0.5; done; exit 0' TERM INT
  tail -n 50 -F "$OLOG/hackme-orders.log" &
  while kill -0 "$PID" 2>/dev/null; do sleep 5 & wait $! || true; done
  echo "[hackme] Orders node exited" >&2
  exit 1
fi
export HACKME_MINER_DAEMON=1
if [[ -n "${HACKME_PORT:-}" ]]; then
  export HACKME_BIND_ADDR="127.0.0.1:${HACKME_PORT}" BASE_URL="http://127.0.0.1:${HACKME_PORT}"
fi
cd "$APP"
PID=""
stop() {
  echo "[hackme] stopping"
  if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
    kill -TERM "$PID" 2>/dev/null || true
    # Give the node time to flush SQLite / accruals (stop_hackme_miner.sh kills after 0.5s).
    for _ in $(seq 1 50); do kill -0 "$PID" 2>/dev/null || break; sleep 0.5; done
  fi
  bash ./stop_hackme_miner.sh >/dev/null 2>&1 || true
  exit 0
}
trap stop TERM INT
bash ./start_hackme_miner.sh
# First run: setup wrote .env into the app dir — move it to the volume so it survives upgrades.
if [[ -f "$APP/.env" && ! -L "$APP/.env" ]]; then
  mv "$APP/.env" "$STATE/.env"
  ln -sfn "$STATE/.env" "$APP/.env"
fi
PID="$(cat "$APP/logs/hackme-node.pid" 2>/dev/null || true)"
if [[ -z "$PID" ]]; then
  echo "[hackme] node did not start (see /state/logs) — retrying in 30s" >&2
  sleep 30
  exit 1
fi
touch "$APP/logs/hackme-node.log"
tail -n 50 -F "$APP/logs/hackme-node.log" &
TAIL=$!
# Exit (and let --restart bring it back) if the node dies.
while [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; do
  sleep 5 & wait $! || true
done
kill "$TAIL" 2>/dev/null || true
echo "[hackme] node exited" >&2
exit 1
