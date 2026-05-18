#!/usr/bin/env bash
# Ephemeral coordinator + main on loopback, then run live integration gates (predeploy, worker health,
# internet preflight, final preflight, security, fuzz release gate with heavy language steps optional).
#
# Usage (from repo root):
#   bash scripts/ops/run_ephemeral_stack_and_gates.sh
#
# Optional:
#   SKIP_HEAVY_FUZZ=1  — skip language matrix / chaos / break / orders multilang in fuzz_release_gate (faster)
#   ADMIN_TOKEN=...    — default is a long dev token (not a placeholder)
#   WORKER_SEC=35      — worker_loop duration before gates that need activity

set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

ADMIN_TOKEN="${ADMIN_TOKEN:-ephemeral-gates-admin-token-32chars!!}"
WORKER_SEC="${WORKER_SEC:-35}"
SKIP_HEAVY_FUZZ="${SKIP_HEAVY_FUZZ:-1}"

pick_free_port() {
	python3 -c "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()"
}

require_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "[ephemeral-gates] missing: $1" >&2
		exit 1
	}
}
require_cmd go
require_cmd curl
require_cmd jq
require_cmd timeout
require_cmd python3
require_cmd rsync

echo "[ephemeral-gates] go test ./... (once before stack; predeploy uses SKIP_GO_TEST=1)"
go test ./... -count=1

MAIN_PORT="$(pick_free_port)"
COORD_PORT="$(pick_free_port)"
MAIN_BASE="http://127.0.0.1:${MAIN_PORT}"
COORD_BASE="http://127.0.0.1:${COORD_PORT}"
WORKDIR="${WORKDIR:-/tmp/hackme-ephemeral-gates-$$}"
RUN_ID="${RUN_ID:-ephemeral_gates_$(date -u +%Y%m%dT%H%M%SZ)}"

echo "[ephemeral-gates] WORKDIR=$WORKDIR MAIN=$MAIN_BASE COORD=$COORD_BASE RUN_ID=$RUN_ID"

rm -rf "$WORKDIR"
mkdir -p "$WORKDIR/data"
rsync -a \
	--exclude '.git/' --exclude 'data' --exclude 'data/' --exclude 'reports/' \
	--exclude 'node_modules/' --exclude 'dist/' --exclude 'backups/' --exclude 'logs/' \
	--exclude '.env' --exclude '.env.*' \
	"$ROOT_DIR/" "$WORKDIR/"
cd "$WORKDIR"

mkdir -p "$WORKDIR/bin"
coord_bin="$WORKDIR/bin/coordinator"
main_bin="$WORKDIR/bin/hackme-node"
go build -o "$coord_bin" ./cmd/coordinator
go build -o "$main_bin" .

coord_log="$WORKDIR/coordinator.log"
main_log="$WORKDIR/main.log"
worker_log="$WORKDIR/worker.log"
coord_pid=""
main_pid=""

kill_port_listeners() {
	local port="$1"
	if command -v fuser >/dev/null 2>&1; then
		fuser -k -TERM "${port}/tcp" >/dev/null 2>&1 || true
		sleep 0.3
		fuser -k -KILL "${port}/tcp" >/dev/null 2>&1 || true
	fi
}

cleanup() {
	local ec=$?
	trap - INT TERM EXIT
	[[ -n "${main_pid:-}" ]] && kill -TERM "$main_pid" 2>/dev/null || true
	[[ -n "${coord_pid:-}" ]] && kill -TERM "$coord_pid" 2>/dev/null || true
	sleep 0.5
	[[ -n "${main_pid:-}" ]] && kill -KILL "$main_pid" 2>/dev/null || true
	[[ -n "${coord_pid:-}" ]] && kill -KILL "$coord_pid" 2>/dev/null || true
	kill_port_listeners "$MAIN_PORT"
	kill_port_listeners "$COORD_PORT"
	wait 2>/dev/null || true
	exit "$ec"
}
trap cleanup INT TERM EXIT

echo "[ephemeral-gates] starting coordinator"
HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}" \
	HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
	HACKME_COORDINATOR_DB="$WORKDIR/data/coordinator.db" \
	"$coord_bin" >>"$coord_log" 2>&1 &
coord_pid=$!

for i in $(seq 1 50); do
	if curl -fsS --max-time 3 "$COORD_BASE/api/network/stats" >/dev/null 2>&1; then
		echo "[ephemeral-gates] coordinator up"
		break
	fi
	if ! kill -0 "$coord_pid" 2>/dev/null; then
		echo "[ephemeral-gates] coordinator died:" >&2
		tail -50 "$coord_log" >&2
		exit 1
	fi
	sleep 0.4
	[[ "$i" == 50 ]] && {
		echo "[ephemeral-gates] coordinator timeout" >&2
		tail -50 "$coord_log" >&2
		exit 1
	}
done

echo "[ephemeral-gates] starting main (P2P self-peer + canonical = self for strict gates)"
HACKME_BIND_ADDR="127.0.0.1:${MAIN_PORT}" \
	HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
	HACKME_POOL_COORDINATOR_URL="$COORD_BASE" \
	HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN" \
	HACKME_CANONICAL_CHAIN_URL="$MAIN_BASE" \
	HACKME_P2P_PEERS="$MAIN_BASE" \
	HACKME_FUZZ_AUTORUN=0 \
	"$main_bin" >>"$main_log" 2>&1 &
main_pid=$!

for i in $(seq 1 80); do
	if curl -fsS --max-time 5 "$MAIN_BASE/api/status" >/dev/null 2>&1; then
		echo "[ephemeral-gates] main up"
		break
	fi
	if ! kill -0 "$main_pid" 2>/dev/null; then
		echo "[ephemeral-gates] main died:" >&2
		tail -80 "$main_log" >&2
		exit 1
	fi
	sleep 0.4
	[[ "$i" == 80 ]] && {
		echo "[ephemeral-gates] main timeout" >&2
		tail -80 "$main_log" >&2
		exit 1
	}
done

st="$(curl -fsS --max-time 10 "$MAIN_BASE/api/status")"
if [[ "$(echo "$st" | jq -r '.has_genesis')" != "true" ]]; then
	echo "[ephemeral-gates] posting genesis"
	curl -fsS --max-time 20 -X POST "$MAIN_BASE/api/genesis" \
		-H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
		-H "Content-Type: application/json" \
		-d '{}' >/dev/null
fi

echo "[ephemeral-gates] waiting for P2P self-peer healthy..."
for i in $(seq 1 40); do
	hc="$(curl -fsS --max-time 8 "$MAIN_BASE/api/p2p/peers" | jq '[.peers[]? | select(.healthy==true)] | length')"
	if [[ "${hc:-0}" -ge 1 ]]; then
		echo "[ephemeral-gates] P2P healthy peers: $hc"
		break
	fi
	sleep 1
	[[ "$i" == 40 ]] && {
		echo "[ephemeral-gates] WARN: no healthy P2P peer yet; strict/internet gates may fail"
		curl -fsS --max-time 8 "$MAIN_BASE/api/p2p/peers" | jq '.' >&2 || true
	}
done

export COORD_URL="$COORD_BASE"
export COORD_ADMIN_TOKEN="$ADMIN_TOKEN"
export WORKER_ID="ephemeral-gates-worker"
export BATCH_SIZE="${BATCH_SIZE:-500000}"
export HASHRATE_GHS="${HASHRATE_GHS:-12.5}"
echo "[ephemeral-gates] worker_loop ${WORKER_SEC}s (activity for predeploy / worker health)"
set +e
timeout "${WORKER_SEC}" bash "$WORKDIR/scripts/ops/worker_loop.sh" >>"$worker_log" 2>&1
set -e

cd "$ROOT_DIR"

failures=0
run_gate() {
	local name="$1"
	shift
	cd "$ROOT_DIR" || exit 1
	# Subshell scripts must not leave us in a different cwd; gates assume repo root.
	if [[ -n "${MAIN_BASE:-}" ]] && [[ "$name" != predeploy_gate ]]; then
		local ok=0
		for _try in $(seq 1 25); do
			if curl -fsS --max-time 5 "${MAIN_BASE}/api/status" >/dev/null 2>&1; then
				ok=1
				break
			fi
			sleep 0.5
		done
		if [[ "$ok" != 1 ]]; then
			echo "[ephemeral-gates] FAIL: main API unreachable before $name (see $main_log)" >&2
			tail -40 "$main_log" >&2 || true
			failures=$((failures + 1))
			return 1
		fi
	fi
	echo ""
	echo "========== [ephemeral-gates] $name =========="
	if "$@"; then
		echo "[ephemeral-gates] OK: $name"
	else
		echo "[ephemeral-gates] FAIL: $name" >&2
		failures=$((failures + 1))
	fi
}

# Same chain for "VPS" and local — satisfies canonical_proxy_smoke tip/pool parity.
run_gate "predeploy_gate" env \
	ADMIN_TOKEN="$ADMIN_TOKEN" \
	SKIP_GO_TEST=1 \
	LOCAL_BASE="$MAIN_BASE" \
	VPS_BASE="$MAIN_BASE" \
	COORD_URL="$COORD_BASE" \
	REQUIRE_WALLET_SOURCE=0 \
	RUN_CORE_GATE=0 \
	RUN_HYBRID_SIGNER_SMOKE=0 \
	bash scripts/ops/predeploy_gate.sh

run_gate "worker_mode_health" env \
	VPS_BASE="$MAIN_BASE" \
	LOCAL_BASE="$MAIN_BASE" \
	COORD_URL="$COORD_BASE" \
	REQUIRE_WORKER_ACTIVITY=1 \
	bash scripts/ops/worker_mode_health.sh

run_gate "internet_preflight" env \
	ADMIN_TOKEN="$ADMIN_TOKEN" \
	BASE="$MAIN_BASE" \
	COORD="$COORD_BASE" \
	REQUIRE_P2P=1 \
	MIN_HEALTHY_PEERS=1 \
	MAX_SYNC_LAG_BLOCKS=3 \
	REQUIRE_COORD_HEALTH=1 \
	RUN_PRIVATE_STAGE=1 \
	RUN_DIFFICULTY_HEALTH=1 \
	RUN_ID="${RUN_ID}_inet" \
	bash scripts/ops/internet_preflight.sh

run_gate "strict_network_preflight" env \
	BASE="$MAIN_BASE" \
	COORD="$COORD_BASE" \
	RUN_ID="${RUN_ID}_strict" \
	bash scripts/ops/strict_network_preflight.sh

run_gate "security_assertions" env \
	BASE="$MAIN_BASE" \
	RUN_ID="${RUN_ID}_sec" \
	bash scripts/tests/security_assertions.sh

FUZZ_ENV=(ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$MAIN_BASE" RUN_REDTEAM_SMOKE=1)
if [[ "$SKIP_HEAVY_FUZZ" == "1" ]]; then
	FUZZ_ENV+=(RUN_LANGUAGE_MATRIX=0 RUN_ORDERS_MULTILANG_AUDIT=0 RUN_LANGUAGE_BREAK_ATTEMPTS=0 RUN_CHAOS_LANG_SECURITY=0)
fi
run_gate "fuzz_release_gate" env "${FUZZ_ENV[@]}" bash scripts/ops/fuzz_release_gate.sh

run_gate "final_preflight" env \
	ADMIN_TOKEN="$ADMIN_TOKEN" \
	BASE="$MAIN_BASE" \
	VPS_BASE="$MAIN_BASE" \
	COORD="$COORD_BASE" \
	SKIP_RELEASE_READINESS_GATE=1 \
	RUN_ID="${RUN_ID}_final" \
	bash scripts/ops/final_preflight.sh

run_gate "rc_freeze_gate (internet+final only)" env \
	ADMIN_TOKEN="$ADMIN_TOKEN" \
	BASE="$MAIN_BASE" \
	VPS_BASE="$MAIN_BASE" \
	COORD="$COORD_BASE" \
	RUN_INTERNET_PREFLIGHT=1 \
	RUN_FINAL_PREFLIGHT=1 \
	RUN_FUZZ_RELEASE_GATE=0 \
	RUN_FUZZ_SUPER_GATE=0 \
	INTERNET_REQUIRE_P2P=1 \
	INTERNET_MIN_HEALTHY_PEERS=1 \
	INTERNET_MAX_SYNC_LAG_BLOCKS=3 \
	INTERNET_REQUIRE_COORD_HEALTH=1 \
	INTERNET_RUN_PRIVATE_STAGE=1 \
	INTERNET_RUN_DIFFICULTY_HEALTH=1 \
	RUN_ID="${RUN_ID}_rc" \
	SKIP_RELEASE_READINESS_GATE=1 \
	bash scripts/ops/rc_freeze_gate.sh

echo ""
echo "[ephemeral-gates] optional: top_pool_readiness (canary, relaxed settlement)"
set +e
PROFILE=canary SETTLEMENT_RELAXED=1 BASE="$MAIN_BASE" COORD="$COORD_BASE" \
	RUN_ID="${RUN_ID}_toppool" bash scripts/ops/top_pool_readiness_gate.sh
tp_ec=$?
set -e
if [[ "$tp_ec" != 0 ]]; then
	echo "[ephemeral-gates] WARN: top_pool_readiness_gate exit $tp_ec (thresholds often miss on ephemeral)" >&2
fi

echo ""
echo "[ephemeral-gates] coordinator log tail:"
tail -15 "$coord_log" || true
echo "[ephemeral-gates] main log tail:"
tail -15 "$main_log" || true

if [[ "$failures" != 0 ]]; then
	echo "[ephemeral-gates] DONE: $failures gate(s) failed" >&2
	exit 1
fi
echo "[ephemeral-gates] DONE: all gates passed"
exit 0
