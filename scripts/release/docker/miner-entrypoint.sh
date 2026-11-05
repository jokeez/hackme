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
