# Длительные проверки сети (soak) — как «почувствовать», держит ли стек

Цель: не разовый `curl`, а **линия времени** — ошибки HTTP, задержки, стагнация `tip_height`, доступность coordinator.

## Инструмент

```bash
# По умолчанию 30 мин, каждые 30 с, BASE=https://hackme.tech
bash scripts/ops/network_stability_soak.sh

# Явно: 2 часа, каждые 60 с, + лёгкий опрос coordinator
BASE=https://hackme.tech \
COORD_URL=https://hackme.tech/pool/coordinator \
DURATION_SEC=7200 INTERVAL_SEC=60 \
bash scripts/ops/network_stability_soak.sh
```

Отчёт: `reports/soak-<RUN_ID>/events.jsonl` (построчный JSON) и `summary.txt`.

## Фазы (рекомендуемые сроки)

| Фаза | Длительность | Что смотреть |
|------|----------------|--------------|
| **A. Быстрый регресс** | уже в репо | `bash scripts/ops/repo_final_selfcheck.sh` (при необходимости `RUN_LOCAL_STACK_SMOKE=1`, `PUBLIC_RO_BASE=…`) |
| **B. Публичный soak** | 30–60 мин | `summary.txt`: `status_http_fail` ≈ 0, `latency_ms_max` не растёт бесконечно, в `events.jsonl` нет спама `tip_regressed` |
| **C. Дневной прогон** | 4–8 ч | То же + вручную пару раз `GET /api/worker/settlement` и дашборд; нет роста «зависших» curl на VPS (`ps` / `htop`) |
| **D. Ночной / выходные** | 24–72 ч | Для прод-релиза; сравнить первый и последний час jsonl (jq), алерты по `status_fail` |

## Как интерпретировать

- **`tip_height` не растёт долго** — нормально, если **выключен** локальный PoH на command node; смотрите `mining` и канонический tip в `global_metrics.chain` при follower-режиме.
- **`tip_regressed`** в логе soak — редкий красный флаг (локальный/канон сбой или смена цепи); разбирать с `policy_hash`, P2P, бэкапами.
- **`work_stats_fail`** — coordinator или nginx до него; проверить `systemctl`, лимиты, недавние деплои.
- **Локальный «рой» воркеров** (нагрузка на coordinator, не публичный DNS):  
  `DEMO_SEC=120 WORKER_COUNT=8 bash scripts/ops/simulate_pool_swarm_local.sh`

## Связь с «держит ли сеть»

Держит ли сеть = **стабильный процент 200**, **предсказуемая задержка**, **отсутствие деградации** за окно B–D. Скрипт не заменяет мониторинг хоста (RAM, FD, nginx), но даёт **воспроизводимый** артефакт для сравнения до/после релиза.
