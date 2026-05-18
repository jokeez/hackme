#!/usr/bin/env bash
set -euo pipefail
# Одной командой: сборка + публичный staging (canonical/coordinator/P2P) + админ-токен.
# Запуск из любого места:
#   ./scripts/ops/run_public_staging_node.sh
# Опционально положите переопределения в .secrets/hackme.public.extra.env (например HACKME_POOL_COORDINATOR_TOKEN или HACKME_P2P_TOKEN с VPS).

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

mkdir -p .secrets
TOKEN_FILE=".secrets/hackme_admin_token"
EXTRA_ENV=".secrets/hackme.public.extra.env"

# Staging URLs по умолчанию — см. README / rc_freeze_gate.
# shellcheck source=/dev/null
source "${ROOT}/scripts/ops/use_public_staging_network_env.sh"

if [[ -z "${HACKME_ADMIN_TOKEN:-}" ]]; then
	if [[ -f "$TOKEN_FILE" ]]; then
		export HACKME_ADMIN_TOKEN="$(tr -d '\n\r' < "$TOKEN_FILE")"
	else
		if command -v openssl >/dev/null 2>&1; then
			export HACKME_ADMIN_TOKEN="$(openssl rand -hex 24)"
		else
			export HACKME_ADMIN_TOKEN="$(printf '%x%x%x%x' "$RANDOM" "$RANDOM" "$RANDOM" "$RANDOM")$(printf '%x%x%x%x' "$RANDOM" "$RANDOM" "$RANDOM" "$RANDOM")"
		fi
		printf '%s\n' "$HACKME_ADMIN_TOKEN" >"$TOKEN_FILE"
		chmod 600 "$TOKEN_FILE"
		echo "[hackme] Создан admin-токен: сохранён в $TOKEN_FILE — сохрани файл или добавь в UI dashboard." >&2
	fi
else
	printf '%s\n' "$HACKME_ADMIN_TOKEN" >"$TOKEN_FILE"
	chmod 600 "$TOKEN_FILE"
fi

export HACKME_BIND_ADDR="${HACKME_BIND_ADDR:-127.0.0.1:8080}"
export HACKME_REQUIRE_ADMIN_TOKEN="${HACKME_REQUIRE_ADMIN_TOKEN:-1}"
export HACKME_DESKTOP_MODE="${HACKME_DESKTOP_MODE:-1}"

# Убираем локальный соло-демо из окружения этого процесса.
unset HACKME_CHAIN_LEADER_LOCAL_POH HACKME_BEGINNER_SOLO HACKME_ALLOW_LOCAL_SOLO 2>/dev/null || true

if [[ -f "$EXTRA_ENV" ]]; then
	echo "[hackme] Загрузка $EXTRA_ENV" >&2
	set -a
	# shellcheck source=/dev/null
	source "$EXTRA_ENV"
	set +a
fi

echo "[hackme] BUILD → hackme-node + minersign …" >&2
go build -o hackme-node .
go build -o minersign ./cmd/minersign

echo "[hackme] LISTEN ${HACKME_BIND_ADDR} → dashboard http://${HACKME_BIND_ADDR}/" >&2
echo "[hackme] Admin token file: $TOKEN_FILE (вставь в поле Admin token в UI или заголовок X-Hackme-Admin-Token)" >&2
echo "[hackme] Для публичного координатора задай HACKME_POOL_COORDINATOR_TOKEN (= HACKME_COORDINATOR_ADMIN_TOKEN на VPS), см. .secrets/hackme.public.extra.env" >&2
echo "[hackme] Desktop: hybrid submit prefers node_ed25519.seed (unified; fleet: HACKME_UNIFIED_MINER_NODE_SEED=0 + HACKME_MINER_ED25519_SEED_HEX). Отключить blend при canonical: HACKME_WALLET_DESKTOP_BLEND_LOCAL=0" >&2
exec ./hackme-node
